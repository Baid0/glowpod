package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Run starts the terminal app and returns when the person quits or ctx ends.
func Run(ctx context.Context, src Source, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := tea.NewProgram(New(ctx, src, opts), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil && ctx.Err() == nil && !errors.Is(err, tea.ErrInterrupted) {
		return fmt.Errorf("terminal app: %w", err)
	}
	return nil
}
