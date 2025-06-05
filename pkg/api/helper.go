package api

import (
	"github.com/cedi/urlshortener/api/v1alpha1"
)

type ShortLink struct {
	Name   string                   `json:"name"`
	Spec   v1alpha1.ShortLinkSpec   `json:"spec,omitempty"`
	Status v1alpha1.ShortLinkStatus `json:"status,omitempty"`
}
