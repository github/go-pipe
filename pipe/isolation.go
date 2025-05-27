package pipe

import (
	"context"
)

type ProcessId int

type IsolationPolicy interface {
	Setup(context.Context, ProcessId) error
	Teardown(context.Context) error
}
