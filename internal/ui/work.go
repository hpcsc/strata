package ui

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

const spinEvery = 100 * time.Millisecond

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type work struct {
	label string
	step  atomic.Pointer[string]
	done  chan struct{}
}

type workTicked struct {
	work *work
}

func (m *Model) startWork(label string, run func(report func(step string)) tea.Msg) tea.Cmd {
	w := &work{label: label, done: make(chan struct{})}
	m.work = w
	return tea.Batch(func() tea.Msg {
		defer close(w.done)
		return run(w.report)
	}, w.tick())
}

func (w *work) report(step string) {
	w.step.Store(&step)
}

func (w *work) text() string {
	if step := w.step.Load(); step != nil {
		return *step
	}
	return w.label
}

func (w *work) tick() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-w.done:
			return nil
		case <-time.After(spinEvery):
			return workTicked{w}
		}
	}
}
