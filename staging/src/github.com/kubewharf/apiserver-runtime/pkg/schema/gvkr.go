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

package schema

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type GroupVersionKindResource struct {
	Group    string
	Version  string
	Kind     string
	Resource string
}

func (gvkr GroupVersionKindResource) GroupResource() schema.GroupResource {
	return schema.GroupResource{Group: gvkr.Group, Resource: gvkr.Resource}
}
func (gvkr GroupVersionKindResource) GroupVersion() schema.GroupVersion {
	return schema.GroupVersion{Group: gvkr.Group, Version: gvkr.Version}
}
func (gvkr GroupVersionKindResource) GroupKind() schema.GroupKind {
	return schema.GroupKind{Group: gvkr.Group, Kind: gvkr.Kind}
}
func (gvkr GroupVersionKindResource) GroupVersionKind() schema.GroupVersionKind {
	return gvkr.GroupVersion().WithKind(gvkr.Kind)
}
func (gvkr GroupVersionKindResource) GroupVersionResource() schema.GroupVersionResource {
	return gvkr.GroupVersion().WithResource(gvkr.Resource)
}
func (gvkr GroupVersionKindResource) String() string {
	return gvkr.Group + "/" + gvkr.Version + ", Kind=" + gvkr.Kind + ",Resource=" + gvkr.Resource
}

func (gvkr GroupVersionKindResource) Validate() (errList field.ErrorList) {
	if len(gvkr.Group) == 0 {
		errList = append(errList, field.Required(field.NewPath("Group"), ""))
	}
	if len(gvkr.Version) == 0 {
		errList = append(errList, field.Required(field.NewPath("Version"), ""))
	}
	if len(gvkr.Kind) == 0 {
		errList = append(errList, field.Required(field.NewPath("Kind"), ""))
	}
	if len(gvkr.Resource) == 0 {
		errList = append(errList, field.Required(field.NewPath("Resource"), ""))
	}
	return errList
}
