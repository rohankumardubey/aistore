// Package test provides tests for common low-level types and utilities for all aistore projects
/*
 * Copyright (c) 2018-2021, NVIDIA CORPORATION. All rights reserved.
 */
package tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/devtools/tassert"
)

func TestParseURLScheme(t *testing.T) {
	testCases := []struct{ expectedScheme, expectedAddress, url string }{
		{"http", "localhost:8080", "http://localhost:8080"},
		{"https", "localhost", "https://localhost"},
		{"", "localhost:8080", "localhost:8080"},
	}

	for _, tc := range testCases {
		scheme, address := cos.ParseURLScheme(tc.url)
		tassert.Errorf(t, scheme == tc.expectedScheme, "expected scheme %s, got %s", tc.expectedScheme, scheme)
		tassert.Errorf(t, address == tc.expectedAddress, "expected address %s, got %s", tc.expectedAddress, address)
	}
}

func TestReparseQuery(t *testing.T) {
	const (
		versionID = "1"
		uuid      = "R9oLVoEsxx"
		basePath  = "/s3/imagenet-tar/oisubset-train-0000.tar"
	)

	r := &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Path: fmt.Sprintf("%s?%s=%s", basePath, cmn.QparamUUID, uuid),
		},
	}
	q := url.Values{}
	q.Add("versionID", versionID)
	r.URL.RawQuery = q.Encode()

	cos.ReparseQuery(r)
	actualVersionID, actualUUID := r.URL.Query().Get("versionID"), r.URL.Query().Get(cmn.QparamUUID)
	tassert.Errorf(t, actualVersionID == versionID, "expected versionID to be %q, got %q", versionID, actualVersionID)
	tassert.Errorf(t, actualUUID == uuid, "expected %s to be %q, got %q", cmn.QparamUUID, uuid, actualUUID)
	tassert.Errorf(t, r.URL.Path == basePath, "expected path to be %q, got %q", basePath, r.URL.Path)
}
