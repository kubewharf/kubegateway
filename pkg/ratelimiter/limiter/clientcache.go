package limiter

import (
	"sync"
	"time"
)

func NewClientCache() *ClientCache {
	return &ClientCache{}
}

type ClientCache struct {
	clientHeartbeats sync.Map
}

func (s *ClientCache) Heartbeat(instance string) {
	s.clientHeartbeats.Store(instance, time.Now())
	return
}

func (s *ClientCache) Delete(instance string) {
	s.clientHeartbeats.Delete(instance)
}

func (s *ClientCache) AllClients() (map[string]time.Time, error) {
	heartbeatTime := map[string]time.Time{}
	s.clientHeartbeats.Range(func(key, value interface{}) bool {
		heartbeatTime[key.(string)] = value.(time.Time)
		return true
	})
	return heartbeatTime, nil
}
