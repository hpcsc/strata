package restack

import "context"

type Sync struct {
	planner     *Planner
	unavailable error
}

func NewSync(planner *Planner, unavailable error) *Sync {
	return &Sync{planner: planner, unavailable: unavailable}
}

func (s *Sync) Available() error {
	return s.unavailable
}

func (s *Sync) Plan(ctx context.Context) (Plan, error) {
	return s.planner.Plan(ctx)
}
