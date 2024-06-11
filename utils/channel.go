package utils

import "context"

// InputOutputChannel helps manage channels with context.
// It enforces a quick release pattern so a worker will stop
// immediately when the context is done, and won't be hanged until
// the channel is closed (if ever).
// By using the helper instead of using channels directly, we can make
// sure our system never hangs on channels.
// This is helpful for two scenarios when close(chan) can't be used:
//  1. We want to stop the worker even if there are still items in the channel.
//  2. There are multiple writers to a channel, so it is difficult to close the channel
//     while avoiding possible panics.
type InputOutputChannel[T any] interface {
	InputChannel[T]
	OutputChannel[T]
}

type WithContex[T any] interface {
	Context() context.Context
	WithContext(ctx context.Context) InputOutputChannel[T]
}

type InputChannel[T any] interface {
	WithContex[T]
	Read() (*T, bool)
}

type OutputChannel[T any] interface {
	WithContex[T]
	Write(value *T) bool
}

type channel[T any] struct {
	ctx    context.Context
	input  <-chan *T
	output chan<- *T
}

func NewInputOutputChannel[T any](ctx context.Context, inputOutput chan *T) InputOutputChannel[T] {
	return &channel[T]{
		ctx:    ctx,
		input:  inputOutput,
		output: inputOutput,
	}
}

func NewInputChannel[T any](ctx context.Context, input <-chan *T) InputChannel[T] {
	return &channel[T]{
		ctx:   ctx,
		input: input,
	}
}

func NewOutputChannel[T any](ctx context.Context, output chan<- *T) OutputChannel[T] {
	return &channel[T]{
		ctx:    ctx,
		output: output,
	}
}

// Context returns the internal context.
func (c *channel[T]) Context() context.Context {
	return c.ctx
}

// WithContext creates a new instance with a different context.
func (c *channel[T]) WithContext(ctx context.Context) InputOutputChannel[T] {
	return &channel[T]{
		ctx:    ctx,
		input:  c.input,
		output: c.output,
	}
}

// Read one value from the channel.
// Returns false if the channel is closed or the context is done.
func (c *channel[T]) Read() (*T, bool) {
	if c.input == nil || c.ctx.Err() != nil {
		return nil, false
	}
	select {
	case <-c.ctx.Done():
		return nil, false
	case val, ok := <-c.input:
		return val, ok
	}
}

// Write one value to the channel.
// Returns false if the context is done.
func (c *channel[T]) Write(value *T) bool {
	if c.output == nil || c.ctx.Err() != nil {
		return false
	}
	select {
	case <-c.ctx.Done():
		return false
	case c.output <- value:
		return true
	}
}
