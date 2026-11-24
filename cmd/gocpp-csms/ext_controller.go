package main

import "github.com/shiv3/gocpp/cmd/gocpp-csms/internal/controller"

func init() {
	serveExtensions = append(serveExtensions, &controller.Ext{})
}
