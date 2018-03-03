package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mongodb/mongo-go-driver/mongo/internal/testutil"
	"github.com/mongodb/mongo-go-driver/mongo/private/auth"
	"github.com/mongodb/mongo-go-driver/mongo/private/roots/addr"
	"github.com/mongodb/mongo-go-driver/mongo/private/roots/connection"
	"github.com/mongodb/mongo-go-driver/mongo/private/roots/topology"
)

func TestTopologyServer(t *testing.T) {
	noerr := func(t *testing.T, err error) {
		if err != nil {
			t.Errorf("Unepexted error: %v", err)
			t.FailNow()
		}
	}

	t.Run("After close, should not return new connection", func(t *testing.T) {
		s, err := topology.NewServer(addr.Addr(*host), serveropts(t)...)
		noerr(t, err)
		err = s.Close()
		noerr(t, err)
		_, err = s.Connection(context.Background())
		if err != connection.ErrPoolClosed {
			t.Errorf("Expected error from getting a connection from closed server, but got %v", err)
		}
	})
	t.Run("Shouldn't be able to get more than max connections", func(t *testing.T) {
		t.Parallel()

		s, err := topology.NewServer(addr.Addr(*host),
			serveropts(
				t,
				topology.WithMaxConnections(func(uint16) uint16 { return 2 }),
				topology.WithMaxIdleConnections(func(uint16) uint16 { return 2 }),
			)...,
		)
		noerr(t, err)
		c1, err := s.Connection(context.Background())
		noerr(t, err)
		defer c1.Close()
		c2, err := s.Connection(context.Background())
		noerr(t, err)
		defer c2.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err = s.Connection(ctx)
		if !strings.Contains(err.Error(), "deadline exceeded") {
			t.Errorf("Expected timeout while trying to open more than max connections, but got %v", err)
		}
	})
	t.Run("Should drain pool when monitor fails", func(t *testing.T) {
		// TODO(GODRIVER-274): Implement this once there is a more testable Dialer.
		t.Skip()
	})
	t.Run("Should drain pool on network error", func(t *testing.T) {
		// TODO(GODRIVER-274): Implement this once there is a more testable Dialer that can return
		// net.Conns that can return specific errors.
		t.Skip()
		t.Run("Read network error", func(t *testing.T) {})
		t.Run("Write network error", func(t *testing.T) {})
	})
	t.Run("Should not drain pool on timeout error", func(t *testing.T) {
		// TODO(GODRIVER-274): Implement this once there is a more testable Dialer that can return
		// net.Conns that can return specific errors.
		t.Skip()
		t.Run("Read network timeout", func(t *testing.T) {})
		t.Run("Write network timeout", func(t *testing.T) {})
	})
	t.Run("Close should close all subscription channels", func(t *testing.T) {
		s, err := topology.NewServer(addr.Addr(*host), serveropts(t)...)
		noerr(t, err)

		var done1, done2 = make(chan struct{}), make(chan struct{})

		sub1, err := s.Subscribe()
		noerr(t, err)

		go func() {
			for range sub1.C {
			}

			close(done1)
		}()

		sub2, err := s.Subscribe()
		noerr(t, err)

		go func() {
			for range sub2.C {
			}

			close(done2)
		}()

		err = s.Close()
		noerr(t, err)

		select {
		case <-done1:
		case <-time.After(50 * time.Millisecond):
			t.Error("Closing server did not close subscription channel 1")
		}

		select {
		case <-done2:
		case <-time.After(50 * time.Millisecond):
			t.Error("Closing server did not close subscription channel 2")
		}
	})
	t.Run("Subscribe after Close should return an error", func(t *testing.T) {
		s, err := topology.NewServer(addr.Addr(*host), serveropts(t)...)
		noerr(t, err)

		sub, err := s.Subscribe()
		noerr(t, err)
		err = s.Close()
		noerr(t, err)

		for range sub.C {
		}

		_, err = s.Subscribe()
		if err != topology.ErrSubscribeAfterClosed {
			t.Errorf("Did not receive expected error. got %v; want %v", err, topology.ErrSubscribeAfterClosed)
		}
	})
}

func serveropts(t *testing.T, opts ...topology.ServerOption) []topology.ServerOption {
	noerr := func(t *testing.T, err error) {
		if err != nil {
			t.Errorf("Unepexted error: %v", err)
			t.FailNow()
		}
	}
	cs := testutil.ConnString(t)
	var connOpts []connection.Option
	if cs.Username != "" || cs.AuthMechanism == auth.GSSAPI {
		cred := &auth.Cred{
			Source:      "admin",
			Username:    cs.Username,
			Password:    cs.Password,
			PasswordSet: cs.PasswordSet,
			Props:       cs.AuthMechanismProperties,
		}

		if cs.AuthSource != "" {
			cred.Source = cs.AuthSource
		} else {
			switch cs.AuthMechanism {
			case auth.GSSAPI, auth.PLAIN:
				cred.Source = "$external"
			default:
				cred.Source = cs.Database
			}
		}

		authenticator, err := auth.CreateAuthenticator(cs.AuthMechanism, cred)
		noerr(t, err)

		connOpts = append(connOpts, connection.WithHandshaker(func(h connection.Handshaker) connection.Handshaker {
			return auth.Handshaker(cs.AppName, h, authenticator)
		}))
	}

	if cs.SSL {
		tlsConfig := connection.NewTLSConfig()

		if cs.SSLCaFileSet {
			err := tlsConfig.AddCACertFromFile(cs.SSLCaFile)
			noerr(t, err)
		}

		if cs.SSLInsecure {
			tlsConfig.SetInsecure(true)
		}

		connOpts = append(connOpts, connection.WithTLSConfig(func(*connection.TLSConfig) *connection.TLSConfig { return tlsConfig }))
	}

	if len(connOpts) > 0 {
		opts = append(opts, topology.WithConnectionOptions(func(opts ...connection.Option) []connection.Option {
			return append(opts, connOpts...)
		}))
	}
	return opts
}