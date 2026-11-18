package mail

import "mpn/internal/installer"

// installerPlan is the identity; it only keeps the tests free of an import of their own.
func installerPlan(p installer.Plan) installer.Plan { return p }
