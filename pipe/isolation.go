package pipe

import (
	"context"
)

type IsolationPolicy interface {
	Setup(context.Context, uint64) error
	Teardown(context.Context) error
}
