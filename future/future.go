package future

type Future[T any] struct {
	Done  chan struct{}
	value T
	err   error
}

func New[T any]() *Future[T] {
	return &Future[T]{Done: make(chan struct{})}
}

func (p *Future[T]) Deliver(value T, err error) {
	p.value, p.err = value, err
	close(p.Done)
}

func (p *Future[T]) Wait() (T, error) {
	<-p.Done
	return p.value, p.err
}
