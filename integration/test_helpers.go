package integration

import (
	"crypto/rand"
	"fmt"
)

// GenerateUUID generates a valid UUID v4 string
func GenerateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	// Set version (4) and variant bits according to RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant is 10

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// WithUUID generates a UUID and stores it in tc.TestData under the given key.
// Use this before GivenFile to make the UUID available for templating.
// Example: tc.WithUUID("targetID").GivenFile("target.org", "* Heading\n:ID: {{.targetID}}")
func (tc *LSPTestContext) WithUUID(key string) *LSPTestContext {
	tc.TestData[key] = GenerateUUID()
	return tc
}
