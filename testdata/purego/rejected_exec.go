package rejected

import "os/exec"

func processFixture() {
	_ = exec.Command("helper")
}
