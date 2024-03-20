package options

import "github.com/spf13/pflag"

type RateLimiterOptions struct {
	RateLimiter          string
	RateLimiterService   string
	Kubeconfig           string
	ClientIdentityPrefix string
}

func NewRateLimiterOptions() *RateLimiterOptions {
	return &RateLimiterOptions{
		RateLimiter: "local",
	}
}

func (o *RateLimiterOptions) Validate() []error {
	return nil
}

func (o *RateLimiterOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.RateLimiter, "ratelimiter", o.RateLimiter, "The rate limiter to use. Possible values: 'local', 'remote'.")
	fs.StringVar(&o.RateLimiterService, "ratelimiter-service", o.RateLimiterService, "The rate limiter service for remote limiter.")
	fs.StringVar(&o.ClientIdentityPrefix, "client-id-prefix", o.RateLimiterService, "The rate limiter client id prefix.")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"Path to kubeconfig file to access rate limiter server (the server addr is set by the rate-limiter-service flag).")
}
