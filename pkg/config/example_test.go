package config_test

import (
	"fmt"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func ExampleNewReaderContainer() {
	l := logger.NewNoop()
	yaml := `
log:
  level: debug
server:
  port: 8080
`
	cfg := config.NewReaderContainer(l, "yaml", strings.NewReader(yaml))

	fmt.Println("Level:", cfg.GetString("log.level"))
	fmt.Println("Port:", cfg.GetInt("server.port"))
	// Output:
	// Level: debug
	// Port: 8080
}

func ExampleNewSchema() {
	type AppConfig struct {
		Server struct {
			Port int    `config:"server.port" default:"8080"`
			Host string `config:"server.host"`
		}
		Log struct {
			Level string `config:"log.level" enum:"debug,info,warn,error" default:"info"`
		}
		Github struct {
			Token string `config:"github.token" validate:"required"`
		}
	}

	schema, err := config.NewSchema(config.WithStructSchema(AppConfig{}))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	_ = schema // Use with container.Validate(schema)
}

func ExampleContainer_Validate() {
	type AppConfig struct {
		Log struct {
			Level string `config:"log.level" enum:"debug,info,warn,error"`
		}
	}

	l := logger.NewNoop()
	cfg := config.NewReaderContainer(l, "yaml", strings.NewReader("log:\n  level: verbose\n"))

	schema, _ := config.NewSchema(config.WithStructSchema(AppConfig{}))

	container, ok := cfg.(*config.Container)
	if !ok {
		return
	}

	result := container.Validate(schema)
	if !result.Valid() {
		fmt.Println(result.Error())
	}
}
