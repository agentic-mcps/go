package gopls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Manager owns the replaceable gopls session for one workspace. It retries at
// most one idempotent read after a terminal sidecar failure. Mutation is never
// replayed because the server may have applied an edit before disconnecting.
type Manager struct {
	lifecycle context.Context
	config    Config

	mu     sync.Mutex
	client *Client
	closed bool
}

// NewManager starts the initial long-lived sidecar session.
func NewManager(lifecycle context.Context, config Config) (*Manager, error) {
	if lifecycle == nil {
		return nil, fmt.Errorf("gopls lifecycle context is nil")
	}
	client, err := Start(lifecycle, config)
	if err != nil {
		return nil, err
	}
	return &Manager{lifecycle: lifecycle, config: config, client: client}, nil
}

// Capabilities returns the currently negotiated sidecar capabilities.
func (m *Manager) Capabilities() (Capabilities, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.client == nil {
		return Capabilities{}, io.ErrClosedPipe
	}
	return m.client.Capabilities(), nil
}

// Request delegates one LSP operation. A terminal failure may be retried once
// only when the caller marks the operation idempotent.
func (m *Manager) Request(ctx context.Context, method string, params, result any, idempotent bool) error {
	client, err := m.current()
	if err != nil {
		return err
	}
	requestErr := client.Request(ctx, method, params, result)
	if requestErr == nil || !idempotent || ctx.Err() != nil || !client.Terminated() {
		return requestErr
	}
	if err := m.replace(ctx, client, false); err != nil {
		return errors.Join(requestErr, fmt.Errorf("restarting gopls: %w", err))
	}
	replacement, err := m.current()
	if err != nil {
		return err
	}
	return replacement.Request(ctx, method, params, result)
}

// Notify sends an event to the current session without replay.
func (m *Manager) Notify(method string, params any) error {
	client, err := m.current()
	if err != nil {
		return err
	}
	return client.Notify(method, params)
}

// Restart replaces the session after configuration or repository identity
// changes. It never replays an operation across the boundary.
func (m *Manager) Restart(ctx context.Context) error {
	client, err := m.current()
	if err != nil {
		return err
	}
	return m.replace(ctx, client, true)
}

// Close shuts down the current session and prevents replacement.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	client := m.client
	m.client = nil
	m.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close(ctx)
}

func (m *Manager) current() (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.client == nil {
		return nil, io.ErrClosedPipe
	}
	return m.client, nil
}

func (m *Manager) replace(ctx context.Context, expected *Client, graceful bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return io.ErrClosedPipe
	}
	if m.client != expected {
		return nil
	}
	if graceful && !expected.Terminated() {
		if err := expected.Close(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	replacement, err := Start(m.lifecycle, m.config)
	if err != nil {
		return err
	}
	m.client = replacement
	return nil
}
