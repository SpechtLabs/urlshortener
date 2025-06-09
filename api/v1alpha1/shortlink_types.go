/*
Copyright 2025.

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

package v1alpha1

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShortlinkSpec defines the desired state of Shortlink.
type ShortlinkSpec struct {
	// Owner is the GitHub user name which created the shortlink
	// +kubebuilder:validation:Required
	Owner string `json:"owner"`

	// Co-Owners are the GitHub user name which can also administrate this shortlink
	// +kubebuilder:validation:Optional
	CoOwners []string `json:"owners,omitempty"`

	// Target specifies the target to which we will redirect
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Target string `json:"target"`

	// RedirectAfter specifies after how many seconds to redirect (Default=3)
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	RedirectAfter int64 `json:"after,omitempty"`

	// Code is the URL Code used for the redirection.
	// leave on default (307) when using the HTML behavior. However, if you whish to use a HTTP 3xx redirect, set to the appropriate 3xx status code
	// +kubebuilder:validation:Enum=200;300;301;302;303;304;305;307;308
	// +kubebuilder:default:=307
	Code int `json:"code,omitempty" enums:"200,300,301,302,303,304,305,307,308"`
}

// ShortlinkStatus defines the observed state of Shortlink.
type ShortlinkStatus struct {
	// Count represents how often this ShortLink has been called
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	Count int `json:"count"`

	//LastModified is a date-time when the ShortLink was last modified
	// +kubebuilder:validation:Format:date-time
	// +kubebuilder:validation:Optional
	LastModified string `json:"lastmodified"`

	// ChangedBy indicates who (GitHub User) changed the Shortlink last
	// +kubebuilder:validation:Optional
	ChangedBy string `json:"changedby"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target`
// +kubebuilder:printcolumn:name="Code",type=string,JSONPath=`.spec.code`
// +kubebuilder:printcolumn:name="After",type=string,JSONPath=`.spec.after`
// +kubebuilder:printcolumn:name="Invoked",type=string,JSONPath=`.status.count`
// +k8s:openapi-gen=true

// Shortlink is the Schema for the shortlinks API.
type Shortlink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShortlinkSpec   `json:"spec,omitempty"`
	Status ShortlinkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ShortlinkList contains a list of Shortlink.
type ShortlinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Shortlink `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Shortlink{}, &ShortlinkList{})
}

// +kubebuilder:object:root=false

// ShortLinkAPI is the API representation of a Shortlink.
type ShortLinkAPI struct {
	Name   string          `json:"name"`
	Spec   ShortlinkSpec   `json:"spec,omitempty"`
	Status ShortlinkStatus `json:"status,omitempty"`
}

func (s *Shortlink) IsOwnedBy(username string) bool {
	return s.Spec.Owner == username || slices.Contains(s.Spec.CoOwners, username)
}
