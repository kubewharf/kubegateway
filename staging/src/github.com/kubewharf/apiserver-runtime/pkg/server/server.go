// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"fmt"
	"time"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/klog"
)

type GenericServer struct {
	Name             string
	GenericAPIServer *genericapiserver.GenericAPIServer
	ServerStarted    <-chan struct{}
	sidecars         []*GenericServer
}

func New(name string, s *genericapiserver.GenericAPIServer) *GenericServer {
	serverStarted := make(chan struct{})
	postStartHookName := "close-server-started-channel"
	s.AddPostStartHookOrDie(postStartHookName, func(context genericapiserver.PostStartHookContext) error {
		// close channel to notify server started
		close(serverStarted)
		return nil
	})
	return &GenericServer{
		Name:             name,
		GenericAPIServer: s,
		ServerStarted:    serverStarted,
	}
}

func (s *GenericServer) AddSidecarServers(sidecars ...*GenericServer) *GenericServer {
	s.sidecars = append(s.sidecars, sidecars...)
	return s
}

func (s *GenericServer) PrepareRun() PreparedServer {
	for i := range s.sidecars {
		sidecar := s.sidecars[i]
		prepared := sidecar.PrepareRun()

		// start sidecar server after control plane started
		postStartHookName := fmt.Sprintf("start-sidecar-server/%v", sidecar.Name)
		s.GenericAPIServer.AddPostStartHookOrDie(postStartHookName, func(context genericapiserver.PostStartHookContext) error {
			errCh := make(chan error)
			klog.Infof("Starting sidecar server %v", sidecar.Name)
			// start sidecar in another goroutine
			go func() {
				err := prepared.Run(context.StopCh)
				if err != nil {
					errCh <- err
					return
				}
			}()

			select {
			case err := <-errCh:
				// return err if failed to start sidecar server
				return err
			case <-sidecar.ServerStarted:
				// return nil after sidecar server started
				return nil
			}
		})

		// waiting for sidecar server shutting down
		preShutdownHookName := fmt.Sprintf("wait-for-sidecar-server-shutdown/%v", sidecar.Name)
		s.GenericAPIServer.AddPreShutdownHookOrDie(preShutdownHookName, func() error {
			klog.Infof("Waiting for sidecar server %v shutdown", sidecar.Name)
			time.Sleep(sidecar.GenericAPIServer.ShutdownDelayDuration)
			sidecar.GenericAPIServer.HandlerChainWaitGroup.Wait()
			return nil
		})
	}

	return s.GenericAPIServer.PrepareRun()
}

type PreparedServer interface {
	Run(stopCh <-chan struct{}) error
}
