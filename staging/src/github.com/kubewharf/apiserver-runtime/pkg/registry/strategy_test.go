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

package registry

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestDefaultStorageStrategy_PrepareForCreate(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
		want runtime.Object
	}{
		{
			"configmap",
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "configmap",
				},
				Data: map[string]string{
					"case": "1",
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "configmap",
					Generation: 1,
				},
				Data: map[string]string{
					"case": "1",
				},
			},
		},
		{
			"deployment",
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployment",
				},

				Status: appsv1.DeploymentStatus{
					Replicas: 1,
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "deployment",
					Generation: 1,
				},
				Status: appsv1.DeploymentStatus{},
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			NamespacedStorageStrategySingleton.PrepareForCreate(context.TODO(), tt.obj)
			if !apiequality.Semantic.DeepEqual(tt.obj, tt.want) {
				t.Errorf("DefaultRESTStrategy.PrepareForCreate failed, obj %+v != want %+v", tt.obj, tt.want)
			}
		})
	}
}

func TestDefaultStorageStrategy_PrepareForUpdate(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
		old  runtime.Object
		want runtime.Object
	}{
		{
			"configmap",
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "configmap",
					Generation: 1,
				},
				Data: map[string]string{
					"case": "1",
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "configmap",
					Generation: 1,
				},
				Data: map[string]string{
					"case": "1",
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "configmap",
					Generation: 1,
				},
				Data: map[string]string{
					"case": "1",
				},
			},
		},
		{
			"deployment",
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployment",
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 2,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 10,
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "deployment",
					Generation: 10,
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 1,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 1,
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "deployment",
					Generation: 11,
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 2,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 1,
				},
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			NamespacedStorageStrategySingleton.PrepareForUpdate(context.TODO(), tt.obj, tt.old)
			if !apiequality.Semantic.DeepEqual(tt.obj, tt.want) {
				t.Errorf("DefaultRESTStrategy.PrepareForCreate failed, obj %+v != want %+v", tt.obj, tt.want)
			}
		})
	}
}

func TestDefaultStatusRESTStrategy_PrepareForUpdate(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
		old  runtime.Object
		want runtime.Object
	}{
		{
			"do not update spec and labels",
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployment",
					Labels: map[string]string{
						"new": "new",
					},
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 2,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 10,
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployment",
					Labels: map[string]string{
						"old": "old",
					},
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 1,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 1,
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deployment",
					Labels: map[string]string{
						"old": "old",
					},
				},
				Spec: appsv1.DeploymentSpec{
					MinReadySeconds: 1,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 10,
				},
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			NamespacedStatusStorageStrategySingleton.PrepareForUpdate(context.TODO(), tt.obj, tt.old)
			if !apiequality.Semantic.DeepEqual(tt.obj, tt.want) {
				t.Errorf("DefaultStatusRESTStrategy.PrepareForUpdate failed, obj %+v != want %+v", tt.obj, tt.want)
			}
		})
	}
}
