/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const AppFolderResourcePlural = "appfolders"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AppFolder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              AppFolderSpec   `json:"spec"`
	Status            AppFolderStatus `json:"status,omitempty"`
}

type AppFolderSpec struct {
	List v1.List `json:"list"`
	Bar  bool    `json:"bar"`
}

type AppFolderStatus struct {
	State   AppFolderState `json:"state,omitempty"`
	Message string         `json:"message,omitempty"`
}

type AppFolderState string

const (
	AppFolderStateCreated   AppFolderState = "Created"
	AppFolderStateProcessed AppFolderState = "Processed"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AppFolderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []AppFolder `json:"items"`
}
