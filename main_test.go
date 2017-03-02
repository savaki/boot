package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRE(t *testing.T) {
	testCases := map[string]struct {
		In     string
		Bucket string
		Prefix string
	}{
		"simple": {
			In:     "s3://hello/world",
			Bucket: "hello",
			Prefix: "world",
		},
		"subdir": {
			In:     "s3://bucket/a/b/c",
			Bucket: "bucket",
			Prefix: "a/b/c",
		},
	}

	for label, tc := range testCases {
		t.Run(label, func(t *testing.T) {
			matches := reS3.FindAllStringSubmatch(tc.In, -1)
			assert.Equal(t, tc.Bucket, matches[0][1])
			assert.Equal(t, tc.Prefix, matches[0][3])
		})
	}
}
