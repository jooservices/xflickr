package fakeflickr

import (
	_ "embed"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

// Dataset is the public seed data exposed by the fake Flickr service.
type Dataset struct {
	People    []Person   `yaml:"people"`
	Photos    []Photo    `yaml:"photos"`
	Favorites []Favorite `yaml:"favorites"`
	Albums    []Album    `yaml:"albums"`
	Contacts  []Contact  `yaml:"contacts"`
	Groups    []Group    `yaml:"groups"`
	Galleries []Gallery  `yaml:"galleries"`
}

//go:embed testdata/seed.yaml
var defaultSeed []byte

type Person struct {
	NSID     string `yaml:"nsid"`
	Username string `yaml:"username"`
	RealName string `yaml:"realname"`
}
type Photo struct {
	ID    string `yaml:"id"`
	Owner string `yaml:"owner"`
	Title string `yaml:"title"`
	Shape string `yaml:"shape"`
}
type Favorite struct {
	Person string `yaml:"person"`
	Photo  string `yaml:"photo"`
	Date   string `yaml:"date"`
}
type Album struct {
	ID     string   `yaml:"id"`
	Owner  string   `yaml:"owner"`
	Title  string   `yaml:"title"`
	Photos []string `yaml:"photos"`
}
type Contact struct {
	Owner  string `yaml:"owner"`
	NSID   string `yaml:"nsid"`
	Friend bool   `yaml:"friend"`
	Family bool   `yaml:"family"`
}
type Group struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Members []string `yaml:"members"`
	Photos  []string `yaml:"photos"`
}
type Gallery struct {
	ID     string   `yaml:"id"`
	Owner  string   `yaml:"owner"`
	Title  string   `yaml:"title"`
	Photos []string `yaml:"photos"`
}

// LoadDataset reads the version-controlled YAML seed.
func LoadDataset(path string) (Dataset, error) {
	contents := defaultSeed
	if path != "" {
		var err error
		contents, err = os.ReadFile(path) //nolint:gosec // callers explicitly configure an alternate seed file.
		if err != nil {
			return Dataset{}, fmt.Errorf("read fake Flickr dataset: %w", err)
		}
	}
	var data Dataset
	if err := yaml.Unmarshal(contents, &data); err != nil {
		return Dataset{}, fmt.Errorf("decode fake Flickr dataset: %w", err)
	}
	if len(data.People) == 0 || len(data.Photos) == 0 {
		return Dataset{}, fmt.Errorf("fake Flickr dataset must contain people and photos")
	}
	return data, nil
}
