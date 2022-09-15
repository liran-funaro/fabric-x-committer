package shardsservice

type goroutineLimiter struct {
	limiter chan struct{}
}

func newGoroutineLimiter(limit uint32) *goroutineLimiter {
	return &goroutineLimiter{
		limiter: make(chan struct{}, limit),
	}
}

func (g *goroutineLimiter) add() {
	g.limiter <- struct{}{}
}

func (g *goroutineLimiter) done() {
	<-g.limiter
}
