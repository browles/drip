package future

type Future[T any] struct {
	Done  chan struct{}
	value T
}

func New[T any]() *Future[T] {
	return &Future[T]{Done: make(chan struct{})}
}

func (p *Future[T]) Deliver(value T) {
	p.value = value
	close(p.Done)
}

func (p *Future[T]) Wait() T {
	<-p.Done
	return p.value
}
