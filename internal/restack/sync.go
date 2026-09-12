package restack

import "context"

type Sync struct {
	planner     *Planner
	mover       *Mover
	unavailable error
}

func NewSync(planner *Planner, mover *Mover, unavailable error) *Sync {
	return &Sync{planner: planner, mover: mover, unavailable: unavailable}
}

func (s *Sync) Available() error {
	return s.unavailable
}

func (s *Sync) Plan(ctx context.Context) (Plan, error) {
	return s.planner.Plan(ctx)
}

func (s *Sync) Move(ctx context.Context, plan Plan) (Result, error) {
	return s.mover.Move(ctx, plan)
}
