package utils

import (
	"os"
	"os/exec"

	"github.com/charmbracelet/log"
)

const (
	InstructionKubectl    = "For instructions on how to install kubectl see: https://kubernetes.io/docs/tasks/tools/"
	InstructionAz         = "For instructions on how to install the Azure CLI see: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli"
	InstructionKubelogin  = "For instructions on how to install kubelogin see: https://azure.github.io/kubelogin/install.html"
	InstructionTerraform  = "For instructions on how to install Terraform see: https://learn.hashicorp.com/tutorials/terraform/install-cli"
	InstructionTerragrunt = "For instructions on how to install Terragrunt see: https://terragrunt.gruntwork.io/docs/getting-started/install/"
	InstructionAws        = "For instructions on how to install the AWS CLI see: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
	InstructionGit        = "For instructions on how to install Git see: https://git-scm.com/book/en/v2/Getting-Started-Installing-Git"
	InstructionGh         = "For instructions on how to install the GitHub CLI see: https://github.com/cli/cli#installation"
)

var (
	Instructions = map[string]string{
		"kubectl":    InstructionKubectl,
		"az":         InstructionAz,
		"kubelogin":  InstructionKubelogin,
		"terraform":  InstructionTerraform,
		"terragrunt": InstructionTerragrunt,
		"aws":        InstructionAws,
		"git":        InstructionGit,
		"gh":         InstructionGh,
	}
)

func GracefulGetPath(name string, logger *log.Logger, instructions ...string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		if i, ok := Instructions[name]; ok {
			logger.Warn(i)
		}

		for _, i := range instructions {
			logger.Warn(i)
		}

		logger.Errorf("the '%s' command is not available, please make sure it is installed and configured in your PATH", name)

		return "", err
	}

	logger.Debugf("using '%s' command at '%s'", name, p)

	return p, nil
}

func IsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}
