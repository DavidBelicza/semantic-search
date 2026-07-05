package strategy

// Pool holds the configured strategies. It is the outermost injected object: the pipeline
// receives the pool and asks it which strategy claims a given file.
type Pool struct {
	strategies []Strategy
}

func NewPool(strategies ...Strategy) Pool {
	return Pool{strategies: strategies}
}

// For returns the first strategy that claims the given path.
func (p Pool) For(path string) (Strategy, bool) {
	for _, strategy := range p.strategies {
		if strategy.Claims(path) {
			return strategy, true
		}
	}

	return nil, false
}
