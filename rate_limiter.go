package main

import "golang.org/x/time/rate"

type TokenBucketRateLimiter struct {
	limiter *rate.Limiter
}

func NewTokenBucketRateLimiter(r rate.Limit, b int) *TokenBucketRateLimiter {
	return &TokenBucketRateLimiter{
		limiter: rate.NewLimiter(r, b),
	}
}

func (l *TokenBucketRateLimiter) RateLimit(in <-chan *Request, out chan<- *Request) {
	defer close(out)
	for req := range in {
		if !l.limiter.Allow() {
			continue
		}
		out <- req
	}
}
