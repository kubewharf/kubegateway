package bootstrap

import (
	"sync"

	genericapiserver "k8s.io/apiserver/pkg/server"
)

var (
	hookLock       sync.Mutex
	postStartHooks = map[string]genericapiserver.PostStartHookFunc{}
)

func AddPostStartHook(name string, hook genericapiserver.PostStartHookFunc) {
	hookLock.Lock()
	postStartHooks[name] = hook
	hookLock.Unlock()
}

func PostStartHooks() map[string]genericapiserver.PostStartHookFunc {
	return postStartHooks
}
