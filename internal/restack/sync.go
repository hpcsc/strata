package restack

import "context"

type Sync struct {
	planner     *Planner
	mover       *Mover
	resolver    *Resolver
	unavailable error
}

func NewSync(planner *Planner, mover *Mover, resolver *Resolver, unavailable error) *Sync {
	return &Sync{planner: planner, mover: mover, resolver: resolver, unavailable: unavailable}
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

func (s *Sync) Pending(ctx context.Context) (Pending, error) {
	return s.resolver.Pending(ctx)
}

func (s *Sync) Resolve(ctx context.Context, plan Plan, branch string) (Pending, error) {
	return s.resolver.Start(ctx, plan, branch)
}

func (s *Sync) Finish(ctx context.Context) (Result, error) {
	return s.resolver.Finish(ctx)
}
