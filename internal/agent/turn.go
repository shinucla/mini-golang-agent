package agent

import "context"

type turnKey struct{}

type Turn struct {
	Owner    string
	Provider string
	Model    string
}

func WithTurn(ctx context.Context, t Turn) context.Context {
	return context.WithValue(ctx, turnKey{}, t)
}

func TurnOf(ctx context.Context) Turn {
	t, _ := ctx.Value(turnKey{}).(Turn)
	return t
}
