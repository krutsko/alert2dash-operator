/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package e2e

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting alert2dash-operator suite\n")
	RunSpecs(t, "e2e suite")
}
