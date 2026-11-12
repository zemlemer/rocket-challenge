// Package integration holds tests that span several layers in process
// (handler + repository + PostgreSQL), without the HTTP server or containers.
// Like other integration tests they are skipped unless TEST_DATABASE_URL is set.
package integration
