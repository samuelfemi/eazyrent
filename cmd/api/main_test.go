package main

import (
	"testing"

	"github.com/femi/golang-easyrent/docs"
)

func TestSetSwaggerHostHTTPS(t *testing.T) {
	origHost, origSchemes := docs.SwaggerInfo.Host, docs.SwaggerInfo.Schemes
	defer func() { docs.SwaggerInfo.Host, docs.SwaggerInfo.Schemes = origHost, origSchemes }()

	setSwaggerHost("https://eazyrent-production-23a4.up.railway.app")

	if docs.SwaggerInfo.Host != "eazyrent-production-23a4.up.railway.app" {
		t.Fatalf("Host: got %q", docs.SwaggerInfo.Host)
	}
	if len(docs.SwaggerInfo.Schemes) != 1 || docs.SwaggerInfo.Schemes[0] != "https" {
		t.Fatalf("Schemes: got %v", docs.SwaggerInfo.Schemes)
	}
}

func TestSetSwaggerHostBadURLKeepsDefault(t *testing.T) {
	origHost := docs.SwaggerInfo.Host
	defer func() { docs.SwaggerInfo.Host = origHost }()

	setSwaggerHost("://bad-url")

	if docs.SwaggerInfo.Host != origHost {
		t.Fatalf("Host changed on bad URL: got %q", docs.SwaggerInfo.Host)
	}
}
