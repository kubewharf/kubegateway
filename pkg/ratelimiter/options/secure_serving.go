package options

import (
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
)

type SecureServingOptions struct {
	SecurePort int
}

func NewSecureServingOptions() *SecureServingOptions {
	return &SecureServingOptions{}
}

func (s *SecureServingOptions) ValidateWith(controlPlaneSecureServingOptions *options.SecureServingOptions, enableControlPlane bool) []error {
	if s == nil {
		return nil
	}
	var errors []error

	if s.SecurePort > 65535 {
		errors = append(errors, fmt.Errorf("ports in --ratelimiter-secure-port %v must be between 1 and 65535", s.SecurePort))
	}
	controlPlanePort := controlPlaneSecureServingOptions.BindPort

	if enableControlPlane && (controlPlanePort <= 0 || controlPlanePort > 65535) {
		errors = append(errors, fmt.Errorf("if --enable-controlplane=true, ports in --secure-port %v must be between 1 and 65535", controlPlanePort))
	}

	if controlPlanePort > 0 && controlPlanePort == s.SecurePort {
		errors = append(errors, fmt.Errorf("ports in --ratelimiter-secure-port %v is duplicate in --secure-port", s.SecurePort))
	}

	return errors
}

func (s *SecureServingOptions) AddFlags(fs *pflag.FlagSet) {
	if s == nil {
		return
	}
	fs.IntVar(&s.SecurePort, "ratelimiter-secure-port", s.SecurePort, "secure port to serve HTTPS for ratelimiter server.")
}

func (s *SecureServingOptions) ApplyTo(
	secureServingInfo **server.SecureServingInfo,
	controlPlaneSecureServingOptions *options.SecureServingOptions,
) error {
	secureServingOptions := cloneSecureServingOptions(controlPlaneSecureServingOptions)
	secureServingOptions.BindPort = s.SecurePort

	// we don't need loopbackconfig for proxy
	return secureServingOptions.ApplyTo(secureServingInfo)
}

func cloneSecureServingOptions(in *options.SecureServingOptions) *options.SecureServingOptions {
	out := *in
	out.Listener = nil
	return &out
}
