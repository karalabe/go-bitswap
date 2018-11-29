package sessionmanager

import (
	"context"
	"sync"

	blocks "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"

	bssession "github.com/ipfs/go-bitswap/session"
	exchange "github.com/ipfs/go-ipfs-exchange-interface"
	peer "github.com/libp2p/go-libp2p-peer"
)

// Session is a session that is managed by the session manager
type Session interface {
	exchange.Fetcher
	InterestedIn(cid.Cid) bool
	ReceiveBlockFrom(peer.ID, blocks.Block)
}

type sesTrk struct {
	session Session
	pm      bssession.PeerManager
}

// SessionFactory generates a new session for the SessionManager to track
type SessionFactory func(ctx context.Context, id uint64, pm bssession.PeerManager) Session

// PeerManagerFactory generates a new peer manager for a session
type PeerManagerFactory func(ctx context.Context, id uint64) bssession.PeerManager

// SessionManager is responsible for creating, managing, and dispatching to
// sessions
type SessionManager struct {
	ctx                context.Context
	sessionFactory     SessionFactory
	peerManagerFactory PeerManagerFactory
	// Sessions
	sessLk   sync.Mutex
	sessions []sesTrk

	// Session Index
	sessIDLk sync.Mutex
	sessID   uint64
}

// New creates a new SessionManager
func New(ctx context.Context, sessionFactory SessionFactory, peerManagerFactory PeerManagerFactory) *SessionManager {
	return &SessionManager{
		ctx:                ctx,
		sessionFactory:     sessionFactory,
		peerManagerFactory: peerManagerFactory,
	}
}

// NewSession initializes a session with the given context, and adds to the
// session manager
func (sm *SessionManager) NewSession(ctx context.Context) exchange.Fetcher {
	id := sm.GetNextSessionID()
	sessionctx, cancel := context.WithCancel(ctx)

	pm := sm.peerManagerFactory(sessionctx, id)
	session := sm.sessionFactory(sessionctx, id, pm)
	tracked := sesTrk{session, pm}
	sm.sessLk.Lock()
	sm.sessions = append(sm.sessions, tracked)
	sm.sessLk.Unlock()
	go func() {
		for {
			defer cancel()
			select {
			case <-sm.ctx.Done():
				sm.removeSession(tracked)
				return
			case <-ctx.Done():
				sm.removeSession(tracked)
				return
			}
		}
	}()

	return session
}

func (sm *SessionManager) removeSession(session sesTrk) {
	sm.sessLk.Lock()
	defer sm.sessLk.Unlock()
	for i := 0; i < len(sm.sessions); i++ {
		if sm.sessions[i] == session {
			sm.sessions[i] = sm.sessions[len(sm.sessions)-1]
			sm.sessions = sm.sessions[:len(sm.sessions)-1]
			return
		}
	}
}

// GetNextSessionID returns the next sequentional identifier for a session
func (sm *SessionManager) GetNextSessionID() uint64 {
	sm.sessIDLk.Lock()
	defer sm.sessIDLk.Unlock()
	sm.sessID++
	return sm.sessID
}

// ReceiveBlockFrom receives a block from a peer and dispatches to interested
// sessions
func (sm *SessionManager) ReceiveBlockFrom(from peer.ID, blk blocks.Block) {
	sm.sessLk.Lock()
	defer sm.sessLk.Unlock()

	k := blk.Cid()
	for _, s := range sm.sessions {
		if s.session.InterestedIn(k) {
			s.session.ReceiveBlockFrom(from, blk)
		}
	}
}
