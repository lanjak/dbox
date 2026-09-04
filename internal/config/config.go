package config

type Plan string

const (
	Starter Plan = "starter"
	Pro     Plan = "pro"
)

type Config struct {
	APIKey      string
	DownloadDir string
	Plan        Plan
}
