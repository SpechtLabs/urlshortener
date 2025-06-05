package api

import (
	"github.com/cedi/urlshortener/api/v1alpha1"
)

type ShortLink struct {
	Name   string                   `json:"name"`
	Spec   v1alpha1.ShortLinkSpec   `json:"spec,omitempty"`
	Status v1alpha1.ShortLinkStatus `json:"status,omitempty"`
}

type GithubUser struct {
	Id        int    `json:"id,omitempty"`
	Login     string `json:"login,omitempty"`
	AvatarUrl string `json:"avatar_url,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
}
