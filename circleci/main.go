package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.braintreeps.com/jszwedko/circleci"

	"github.com/codegangsta/cli"
	"github.com/fatih/color"
)

var (
	// VersionString is the git tag this binary is associated with
	VersionString string
	// RevisionString is the git rev this binary is associated with
	RevisionString string

	// Client is the client for interacting with CircleCI
	Client *circleci.Client

	successSprintf  = color.New(color.FgGreen).SprintfFunc()
	failureSprintf  = color.New(color.FgRed).SprintfFunc()
	notestsSprintf  = color.New(color.FgYellow).SprintfFunc()
	nobuildsSprintf = color.New(color.FgYellow).SprintfFunc()
	noneSprintf     = color.New(color.FgWhite).SprintfFunc()
	runningSprintf  = color.New(color.FgBlue).SprintfFunc()
	resetSprintf    = color.New(color.Reset).SprintfFunc()
)

func statusSprintfFunc(status string) sprintf {
	switch status {
	case "no_tests", "canceled":
		return notestsSprintf
	case "success", "fixed":
		return successSprintf
	case "failed", "timedout", "failure":
		return failureSprintf
	case "running":
		return runningSprintf
	default:
		return noneSprintf
	}
}

type sprintf func(format string, a ...interface{}) string

// Filter is an "enum" for build query filters that can be used as a cli.Generic
type Filter string

var validFilters = []string{"completed", "successful", "failed", "running"}

// Set satifies the cli.Generic interface
// Ensures that the value is one of the valid filters
func (f *Filter) Set(value string) error {
	for _, filter := range validFilters {
		if filter == value {
			*f = Filter(value)
			return nil
		}
	}

	return fmt.Errorf("must be one of %s", strings.Join(validFilters, ","))
}

// String returns the filter
func (f *Filter) String() string {
	return string(*f)
}

// Project is meant to be used as a cli.Generic to parse <account>/<repo> strings
type Project struct {
	Account    string
	Repository string
}

// Set satifies the cli.Generic interface
// Parses the value into the account and repo
func (p *Project) Set(value string) error {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) < 2 {
		return fmt.Errorf("could not parse %s as '<account>/<repo>'", value)
	}

	p.Account = parts[0]
	p.Repository = parts[1]

	return nil
}

// String returns <account>/<repo>
func (p *Project) String() string {
	if p.Account == "" && p.Repository == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", p.Account, p.Repository)
}

func main() {
	app := cli.NewApp()
	app.Name = "circleci"
	app.Usage = "Tool for interacting with the CircleCI API"
	app.Version = fmt.Sprintf("%s (%s)", VersionString, RevisionString)
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "host, H",
			Value:  "https://circleci.com",
			Usage:  "CircleCI URI",
			EnvVar: "CIRCLE_HOST",
		},
		cli.StringFlag{
			Name:   "token, t",
			Value:  "",
			Usage:  "API token to use to access CircleCI (not needed for displaying information about public repositories)",
			EnvVar: "CIRCLE_TOKEN",
		},
	}
	app.Before = func(c *cli.Context) (err error) {
		baseURL, err := url.Parse(c.String("host") + "/api/v1/")
		if err != nil {
			return err
		}
		Client = &circleci.Client{
			Token:   c.String("token"),
			BaseURL: baseURL,
		}
		return nil
	}
	app.Commands = []cli.Command{
		cli.Command{
			Name:  "projects",
			Usage: "Print projects",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:   "verbose, v",
					Usage:  "Show additional information about projects",
					EnvVar: "CIRCLE_VERBOSE",
				},
				cli.GenericFlag{
					Name:   "project, p",
					Usage:  "Only print one project (useful with --verbose)",
					EnvVar: "CIRCLE_PROJECT",
					Value:  &Project{},
				},
			},
			Action: func(c *cli.Context) {
				projects, err := Client.ListProjects()
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				t := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
				for _, project := range projects {
					if c.IsSet("project") {
						projectFlag := c.Generic("project").(*Project)
						if project.Username != projectFlag.Account || project.Reponame != projectFlag.Repository {
							continue
						}
					}

					if c.Bool("verbose") {
						fmt.Fprintf(t, "%s/%s\f", project.Username, project.Reponame)
					} else {
						projectColorSprintf := nobuildsSprintf
						if len(project.Branches[project.DefaultBranch].RecentBuilds) > 0 {
							projectColorSprintf = statusSprintfFunc(project.Branches[project.DefaultBranch].RecentBuilds[0].Status)
						}
						fmt.Fprintf(t, projectColorSprintf("%s/%s\f", project.Username, project.Reponame))
					}

					if !c.Bool("verbose") {
						continue
					}

					for name, branch := range project.Branches {
						if len(branch.RecentBuilds) == 0 {
							continue
						}
						build := branch.RecentBuilds[0]

						branchMarker := ""
						if name == project.DefaultBranch {
							branchMarker = "*"
						}
						fmt.Fprint(t, statusSprintfFunc(build.Status)("%s%s", name, branchMarker))
						fmt.Fprint(t, statusSprintfFunc(build.Status)("\t%s", build.Status))
						fmt.Fprintln(t)
					}
					fmt.Fprint(t, "\f")
				}
			},
		},
		cli.Command{
			Name:    "recent-builds",
			Aliases: []string{"recent"},
			Usage:   "Recent builds for the current project",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:   "limit, l",
					Value:  30,
					Usage:  "Maximum of builds to return -- set to -1 for no limit",
					EnvVar: "CIRCLE_LIMIT",
				},
				cli.IntFlag{
					Name:   "offset, o",
					Value:  0,
					Usage:  "Offset in results to start at",
					EnvVar: "CIRCLE_OFFSET",
				},
				cli.BoolFlag{
					Name:   "all, a",
					Usage:  "Show builds for all projects; overrides --project",
					EnvVar: "CIRCLE_ALL_BUILDS",
				},
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Show all builds for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.StringFlag{
					Name:   "branch, b",
					Value:  "",
					Usage:  "Show only builds on specified branch (cannot be used with --all); leave empty for all",
					EnvVar: "CIRCLE_BRANCH",
				},
				cli.GenericFlag{
					Name:   "filter, f",
					Value:  new(Filter),
					Usage:  fmt.Sprintf("Show only builds with given status (cannot be used with --all); leave empty for all; must be one of %s", strings.Join(validFilters, ",")),
					EnvVar: "CIRCLE_FILTER",
				},
			},
			Action: func(c *cli.Context) {
				if c.Bool("all") {
					for _, flag := range []string{"project", "branch", "status"} {
						if c.IsSet(flag) {
							fmt.Fprintf(os.Stderr, "--%s cannot be used with --all\n", flag)
							os.Exit(1)
						}
					}
				}

				var (
					builds []*circleci.Build
					err    error
				)
				if c.Bool("all") {
					builds, err = Client.ListRecentBuilds(c.Int("limit"), c.Int("offset"))
				} else {
					project := c.Generic("project").(*Project)
					builds, err = Client.ListRecentBuildsForProject(
						project.Account,
						project.Repository,
						c.String("branch"),
						c.String("status"),
						c.Int("limit"),
						c.Int("offset"))
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				t := tabwriter.NewWriter(os.Stdout, 0, 8, 4, ' ', tabwriter.StripEscape)
				for _, build := range builds {
					fmt.Fprintf(t, "%s/%s/%d\t%s\t%s\t%s\n", build.Username, build.Reponame, build.BuildNum, statusSprintfFunc(build.Status)("\xff%s\xff", build.Status), build.Branch, build.Subject)
				}
				t.Flush()
			},
		},
		cli.Command{
			Name:  "show",
			Usage: "Show details for build",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Show build for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.IntFlag{
					Name:   "build-num, n",
					Value:  0,
					Usage:  "Show details for specified build num (leave empty for latest)",
					EnvVar: "CIRCLE_BUILD_NUM",
				},
				cli.IntFlag{
					Name:   "build-node, i",
					Value:  0,
					Usage:  "For parallel builds, only show the build for the specified node",
					EnvVar: "CIRCLE_BUILD_NODE",
				},
				cli.BoolFlag{
					Name:   "verbose, v",
					Usage:  "Show step output",
					EnvVar: "CIRCLE_VERBOSE",
				},
			},
			Action: func(c *cli.Context) {
				var (
					build   *circleci.Build
					err     error
					project = c.Generic("project").(*Project)
				)

				if !c.IsSet("build-num") {
					builds, err := Client.ListRecentBuildsForProject(project.Account, project.Repository, "", "", 1, 0)
					if err != nil {
						fmt.Fprintln(os.Stderr, err.Error())
						os.Exit(1)
					}

					if len(builds) == 0 {
						fmt.Fprintln(os.Stderr, "no builds")
						os.Exit(1)
					}

					build = builds[0]
				} else {
					build, err = Client.GetBuild(project.Account, project.Repository, c.Int("build-num"))
					if err != nil {
						fmt.Fprintln(os.Stderr, err.Error())
						os.Exit(1)
					}
				}

				t := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
				fmt.Fprintf(t, "Subject\t%s\n", build.Subject)
				fmt.Fprintf(t, "Trigger\t%s\n", build.Why)
				fmt.Fprintf(t, "Author\t%s\n", build.AuthorName)
				fmt.Fprintf(t, "Committer\t%s\n", build.CommitterName)
				fmt.Fprintf(t, "Status\t%s\n", build.Status)

				fmt.Fprintf(t, "Build Parameters\t\n")
				if len(build.BuildParameters) == 0 {
					fmt.Fprintf(t, "\tNone\n")
				}
				for key, value := range build.BuildParameters {
					fmt.Fprintf(t, "\t%s\t%s\n", key, value)
				}

				if build.StartTime == nil {
					fmt.Fprintf(t, "Started\t\n")
				} else {
					fmt.Fprintf(t, "Started\t%s\n", build.StartTime)
				}

				if build.StartTime != nil && build.StopTime != nil {
					fmt.Fprintf(t, "Duration\t%s\n", build.StopTime.Sub(*build.StartTime))
				}
				t.Flush()

				if c.IsSet("build-node") {
					fmt.Println()
					printBuild(build, c.Int("build-node"), c.Bool("verbose"))
				} else {
					for i := 0; i < build.Parallel; i++ {
						fmt.Printf("\nBuild %d\n", i)
						printBuild(build, i, c.Bool("verbose"))
					}
				}
			},
		},
		cli.Command{
			Name:    "list-artifacts",
			Aliases: []string{"artifacts"},
			Usage:   "Show artifacts for build (default to latest)",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Show artifacts for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.IntFlag{
					Name:   "build-num, n",
					Value:  0,
					Usage:  "Show artifacts for specified build num (leave empty for latest)",
					EnvVar: "CIRCLE_BUILD_NUM",
				},
			},
			Action: func(c *cli.Context) {
				var buildNum int

				project := c.Generic("project").(*Project)
				if !c.IsSet("build-num") {
					builds, err := Client.ListRecentBuildsForProject(project.Account, project.Repository, "", "", 1, 0)
					if err != nil {
						fmt.Fprintln(os.Stderr, err.Error())
						os.Exit(1)
					}

					if len(builds) == 0 {
						fmt.Fprintln(os.Stderr, "no builds")
						os.Exit(1)
					}

					buildNum = builds[0].BuildNum
				} else {
					buildNum = c.Int("build-num")
				}

				artifacts, err := Client.ListBuildArtifacts(project.Account, project.Repository, buildNum)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				t := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
				fmt.Fprintf(t, "Node\tPath\tURL\n")
				for _, artifact := range artifacts {
					fmt.Fprintf(t, "%d\t%s\t%s\n", artifact.NodeIndex, artifact.Path, artifact.URL)
				}
				t.Flush()
			},
		},
		cli.Command{
			Name:  "test-metadata",
			Usage: "Show test metadata for build",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Show build for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.IntFlag{
					Name:   "build-num, n",
					Value:  0,
					Usage:  "Show test metadata for specified build num (leave empty for latest)",
					EnvVar: "CIRCLE_BUILD_NUM",
				},
			},
			Action: func(c *cli.Context) {
				var buildNum int

				project := c.Generic("project").(*Project)
				buildNum = c.Int("build-num")
				if !c.IsSet("build-num") {
					buildNum = latestBuild(project).BuildNum
				}

				metadata, err := Client.ListTestMetadata(project.Account, project.Repository, buildNum)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				for _, metadatum := range metadata {
					fmt.Printf("%s: %s %s (%s)\n",
						metadatum.File,
						metadatum.Name,
						statusSprintfFunc(metadatum.Result)(metadatum.Result),
						time.Duration(int(metadatum.RunTime*1000000))*time.Microsecond)
					if metadatum.Message != nil {
						fmt.Println(*metadatum.Message)
					}
				}
			},
		},
		cli.Command{
			Name:    "retry-build",
			Aliases: []string{"retry"},
			Usage:   "Retry a build",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Show build for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.IntFlag{
					Name:   "build-num, n",
					Value:  0,
					Usage:  "Retry specified build num (leave empty for latest)",
					EnvVar: "CIRCLE_BUILD_NUM",
				},
			},
			Action: func(c *cli.Context) {
				project := c.Generic("project").(*Project)

				buildNum := c.Int("build-num")
				if !c.IsSet("build-num") {
					buildNum = latestBuild(project).BuildNum
				}

				build, err := Client.RetryBuild(project.Account, project.Repository, buildNum)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Println(buildURL(build, c.GlobalString("host")))
			},
		},
		cli.Command{
			Name:    "cancel-build",
			Aliases: []string{"cancel"},
			Usage:   "Cancel a build",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Cancel build for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.IntFlag{
					Name:   "build-num, n",
					Value:  0,
					Usage:  "Retry specified build num (leave empty for latest)",
					EnvVar: "CIRCLE_BUILD_NUM",
				},
			},
			Action: func(c *cli.Context) {
				project := c.Generic("project").(*Project)
				buildNum := c.Int("build-num")
				if !c.IsSet("build-num") {
					buildNum = latestBuild(project).BuildNum
				}

				build, err := Client.CancelBuild(project.Account, project.Repository, buildNum)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Printf("canceled build %d\n", build.BuildNum)
			},
		},
		cli.Command{
			Name:  "build",
			Usage: "Trigger a new build",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Trigger build for specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
				cli.StringFlag{
					Name:   "branch, b",
					Value:  "",
					Usage:  "Branch to trigger build on (leave empty for default branch)",
					EnvVar: "CIRCLE_BRANCH",
				},
			},
			Action: func(c *cli.Context) {
				project := c.Generic("project").(*Project)

				branch := c.String("branch")
				if !c.IsSet("branch") {
					p, err := Client.GetProject(project.Account, project.Repository)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						os.Exit(1)
					}
					branch = p.DefaultBranch
				}

				build, err := Client.Build(project.Account, project.Repository, branch)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Println(buildURL(build, c.GlobalString("host")))
			},
		},
		cli.Command{
			Name:  "clear-cache",
			Usage: "Clear the build cache",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Clear cache of specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
			},
			Action: func(c *cli.Context) {
				project := c.Generic("project").(*Project)

				status, err := Client.ClearCache(project.Account, project.Repository)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Println(status)
			},
		},
		cli.Command{
			Name:  "add-env-var",
			Usage: "Add an environment variable to the project (expects the name and value as arguments)",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Add env var to specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
			},
			Action: func(c *cli.Context) {
				if len(c.Args()) != 2 {
					fmt.Fprintln(os.Stderr, "must specify name and value")
					os.Exit(1)
				}

				name, value := c.Args().Get(0), c.Args().Get(1)
				project := c.Generic("project").(*Project)

				_, err := Client.AddEnvVar(project.Account, project.Repository, name, value)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Printf("added %s=%s\n", name, value)
			},
		},
		cli.Command{
			Name:  "delete-env-var",
			Usage: "Add an environment variable to the project (expects the name as argument)",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Delete env var from specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
			},
			Action: func(c *cli.Context) {
				if len(c.Args()) != 1 {
					fmt.Fprintln(os.Stderr, "must specify name")
					os.Exit(1)
				}

				name := c.Args().Get(0)
				project := c.Generic("project").(*Project)

				err := Client.DeleteEnvVar(project.Account, project.Repository, name)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Printf("deleted %s\n", name)
			},
		},
		cli.Command{
			Name:  "add-ssh-key",
			Usage: "Add an SSH key to be used to access external systems (expects the hostname and private key as arguments)",
			Flags: []cli.Flag{
				cli.GenericFlag{
					Name:   "project, p",
					Value:  currentProject(),
					Usage:  "Add SSH key to specified project rather than the current",
					EnvVar: "CIRCLE_PROJECT",
				},
			},
			Action: func(c *cli.Context) {
				if len(c.Args()) != 2 {
					fmt.Fprintln(os.Stderr, "must specify hostname and private key")
					os.Exit(1)
				}

				hostname, privateKey := c.Args().Get(0), c.Args().Get(1)
				project := c.Generic("project").(*Project)

				err := Client.AddSSHKey(project.Account, project.Repository, hostname, privateKey)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}

				fmt.Printf("added key for %s\n", hostname)
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprint(os.Stderr, err)
	}
}

func currentProject() *Project {
	output, err := exec.Command("git", "remote", "-v").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not determine current project, %s: %s\n", err, string(output))
		return &Project{}
	}

	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "origin") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 3 {
			fmt.Fprintf(os.Stderr, "warning: could not determine current project, unexpected number of fields in %s\n", line)
			return &Project{}
		}

		parts := strings.Split(fields[1], ":")
		parts = strings.Split(parts[len(parts)-1], "/")
		if len(parts) < 2 {
			fmt.Fprintf(os.Stderr, "warning: could not determine current project, expected  / in %s\n", fields[1])
			return &Project{}
		}

		return &Project{Account: parts[len(parts)-2], Repository: strings.TrimSuffix(parts[len(parts)-1], ".git")}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not determine current project: %s\n", err)
		return &Project{}
	}

	fmt.Fprintln(os.Stderr, "warning: could not determine current project: no origin set")
	return &Project{}
}

func printBuild(build *circleci.Build, i int, verbose bool) {
	for _, step := range build.Steps {
		action := step.Actions[0]
		if action.Parallel {
			action = step.Actions[i]
		}

		colorSprintfFunc := statusSprintfFunc(action.Status)
		fmt.Print(colorSprintfFunc("* %s (%s)", step.Name, action.Status))
		if action.StartTime != nil && action.EndTime != nil {
			fmt.Print(colorSprintfFunc(" (%s)", action.EndTime.Sub(*action.StartTime)))
		}
		fmt.Println()

		if action.Command != step.Name {
			fmt.Printf("\t%s\n", action.Command)
		}

		if verbose && action.HasOutput {
			outputs, err := Client.GetActionOutput(action)
			if err != nil {
				fmt.Println("error retrieving action output: %s", err)
			}
			for _, output := range outputs {
				fmt.Println(output.Message)
			}
			fmt.Println()
		}
	}
}

func buildURL(build *circleci.Build, host string) string {
	return fmt.Sprintf("%s/gh/%s/%s/%d", host, build.Username, build.Reponame, build.BuildNum)
}

func latestBuild(project *Project) *circleci.Build {
	builds, err := Client.ListRecentBuildsForProject(project.Account, project.Repository, "", "", 1, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if len(builds) == 0 {
		fmt.Fprintln(os.Stderr, fmt.Errorf("no builds"))
		os.Exit(1)
	}
	return builds[0]
}
