package pods

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/square/p2/Godeps/_workspace/src/gopkg.in/yaml.v2"
	"github.com/square/p2/pkg/hoist"
	"github.com/square/p2/pkg/launch"
	"github.com/square/p2/pkg/runit"
	"github.com/square/p2/pkg/types"
	"github.com/square/p2/pkg/util"

	. "github.com/square/p2/Godeps/_workspace/src/github.com/anthonybishopric/gotcha"
)

func getTestPod() *Pod {
	return NewPod(types.PodID("hello"), "/data/pods/test")
}

func getTestPodManifest(t *testing.T) Manifest {
	testPath := util.From(runtime.Caller(0)).ExpandPath("test_manifest.yaml")
	pod, err := ManifestFromPath(testPath)
	Assert(t).IsNil(err, "couldn't read test manifest")
	return pod
}

func getUpdatedManifest(t *testing.T) Manifest {
	podPath := util.From(runtime.Caller(0)).ExpandPath("updated_manifest.yaml")
	pod, err := ManifestFromPath(podPath)
	Assert(t).IsNil(err, "couldn't read test manifest")
	return pod
}

func getLaunchableStanzasFromTestManifest(t *testing.T) map[string]LaunchableStanza {
	return getTestPodManifest(t).GetLaunchableStanzas()
}

func TestGetLaunchable(t *testing.T) {
	launchableStanzas := getLaunchableStanzasFromTestManifest(t)
	pod := getTestPod()
	Assert(t).AreNotEqual(0, len(launchableStanzas), "Expected there to be at least one launchable stanza in the test manifest")
	for _, stanza := range launchableStanzas {
		l, _ := pod.getLaunchable(stanza, "foouser", runit.RestartPolicyAlways)
		launchable := l.(hoist.LaunchAdapter).Launchable
		Assert(t).AreEqual("app", launchable.Id, "Launchable Id did not have expected value")
		Assert(t).AreEqual("hello__app", launchable.ServiceId, "Launchable ServiceId did not have expected value")
		Assert(t).AreEqual("hoisted-hello_def456.tar.gz", launchable.Location, "Launchable location did not have expected value")
		Assert(t).AreEqual("foouser", launchable.RunAs, "Launchable run as did not have expected username")
		Assert(t).IsTrue(launchable.ExecNoLimit, "GetLaunchable() should always set ExecNoLimit to true for hoist launchables")
		Assert(t).AreEqual(launchable.RestartPolicy, runit.RestartPolicyAlways, "Default RestartPolicy for a launchable should be 'always'")
	}
}

func TestPodCanWriteEnvFile(t *testing.T) {
	envDir, err := ioutil.TempDir("", "envdir")
	Assert(t).IsNil(err, "Should not have been an error writing the env dir")
	defer os.RemoveAll(envDir)

	curUser, err := user.Current()
	Assert(t).IsNil(err, "There should not have been an error finding the current user")
	uid, err := strconv.ParseInt(curUser.Uid, 10, 0)
	Assert(t).IsNil(err, "There should not have been an error converting the UID to an int")
	gid, err := strconv.ParseInt(curUser.Gid, 10, 0)
	Assert(t).IsNil(err, "There should not have been an error converting the UID to an int")

	err = writeEnvFile(envDir, "ENVIRONMENT", "staging", int(uid), int(gid))
	Assert(t).IsNil(err, "There should not have been an error writing the config file")

	expectedWritten := filepath.Join(envDir, "ENVIRONMENT")
	file, err := os.Open(expectedWritten)
	defer file.Close()
	Assert(t).IsNil(err, "There should not have been an error when opening the config file")
	contents, err := ioutil.ReadAll(file)
	Assert(t).IsNil(err, "There should not have been an error reading the content of the config file")
	Assert(t).AreEqual("staging", string(contents), "the config file should have been the value 'staging'")
}

func TestPodSetupConfigWritesFiles(t *testing.T) {
	manifestStr := `id: thepod
launchables:
  my-app:
    launchable_type: hoist
    launchable_id: web
    location: https://localhost:4444/foo/bar/baz.tar.gz
    cgroup:
      cpus: 4
      memory: 4G
config:
  ENVIRONMENT: staging
`
	currUser, err := user.Current()
	Assert(t).IsNil(err, "Could not get the current user")
	manifestStr += fmt.Sprintf("run_as: %s", currUser.Username)
	manifest, err := ManifestFromBytes(bytes.NewBufferString(manifestStr).Bytes())
	Assert(t).IsNil(err, "should not have erred reading the manifest")

	podTemp, _ := ioutil.TempDir("", "pod")

	pod := NewPod(manifest.ID(), PodPath(podTemp, manifest.ID()))

	launchables := make([]launch.Launchable, 0)
	for _, stanza := range manifest.GetLaunchableStanzas() {
		launchable, err := pod.getLaunchable(stanza, manifest.RunAsUser(), manifest.GetRestartPolicy())
		Assert(t).IsNil(err, "There shouldn't have been an error getting launchable")
		launchables = append(launchables, launchable)
	}
	err = pod.setupConfig(manifest, launchables)
	Assert(t).IsNil(err, "There shouldn't have been an error setting up config")

	configFileName, err := manifest.ConfigFileName()
	Assert(t).IsNil(err, "Couldn't generate config filename")
	configPath := filepath.Join(pod.ConfigDir(), configFileName)
	config, err := ioutil.ReadFile(configPath)
	Assert(t).IsNil(err, "should not have erred reading the config")
	Assert(t).AreEqual("ENVIRONMENT: staging\n", string(config), "the config didn't match")

	env, err := ioutil.ReadFile(filepath.Join(pod.EnvDir(), "CONFIG_PATH"))
	Assert(t).IsNil(err, "should not have erred reading the env file")
	Assert(t).AreEqual(configPath, string(env), "The env path to config didn't match")

	platformConfigFileName, err := manifest.PlatformConfigFileName()
	Assert(t).IsNil(err, "Couldn't generate platform config filename")
	platformConfigPath := filepath.Join(pod.ConfigDir(), platformConfigFileName)
	platConfig, err := ioutil.ReadFile(platformConfigPath)
	Assert(t).IsNil(err, "should not have erred reading the platform config")

	expectedPlatConfig := `web:
  cgroup:
    cpus: 4
    memory: 4294967296
`
	Assert(t).AreEqual(expectedPlatConfig, string(platConfig), "the platform config didn't match")

	platEnv, err := ioutil.ReadFile(filepath.Join(pod.EnvDir(), "PLATFORM_CONFIG_PATH"))
	Assert(t).IsNil(err, "should not have erred reading the platform config env file")
	Assert(t).AreEqual(platformConfigPath, string(platEnv), "The env path to platform config didn't match")

	for _, launchable := range launchables {
		launchableIdEnv, err := ioutil.ReadFile(filepath.Join(launchable.EnvDir(), "LAUNCHABLE_ID"))
		Assert(t).IsNil(err, "should not have erred reading the launchable ID env file")
		Assert(t).AreEqual(launchable.ID(), string(launchableIdEnv), "The launchable ID did not match expected")

		launchableRootEnv, err := ioutil.ReadFile(filepath.Join(launchable.EnvDir(), "LAUNCHABLE_ROOT"))
		Assert(t).IsNil(err, "should not have erred reading the launchable root env file")
		Assert(t).AreEqual(launchable.InstallDir(), string(launchableRootEnv), "The launchable root path did not match expected")
	}
}

func TestLogLaunchableError(t *testing.T) {
	out := bytes.Buffer{}
	Log.SetLogOut(&out)

	testLaunchable := &hoist.Launchable{ServiceId: "TestLaunchable"}
	testManifest := getTestPodManifest(t)
	testErr := util.Errorf("Unable to do something")
	message := "Test error occurred"
	pod := PodFromManifestId(testManifest.ID())
	pod.logLaunchableError(testLaunchable.ServiceId, testErr, message)

	output, err := ioutil.ReadAll(&out)
	Assert(t).IsNil(err, "Got an error reading the logging output")
	outputString := bytes.NewBuffer(output).String()
	Assert(t).Matches(outputString, ContainsString("TestLaunchable"), "Expected 'TestLaunchable' to appear somewhere in log output")
	Assert(t).Matches(outputString, ContainsString("hello"), "Expected 'hello' to appear somewhere in log output")
	Assert(t).Matches(outputString, ContainsString("Test error occurred"), "Expected error message to appear somewhere in log output")
}

func TestLogError(t *testing.T) {
	out := bytes.Buffer{}
	Log.SetLogOut(&out)

	testManifest := getTestPodManifest(t)
	testErr := util.Errorf("Unable to do something")
	message := "Test error occurred"
	pod := PodFromManifestId(testManifest.ID())
	pod.logError(testErr, message)

	output, err := ioutil.ReadAll(&out)
	Assert(t).IsNil(err, "Got an error reading the logging output")
	outputString := bytes.NewBuffer(output).String()
	Assert(t).Matches(outputString, ContainsString("hello"), "Expected 'hello' to appear somewhere in log output")
	Assert(t).Matches(outputString, ContainsString("Test error occurred"), "Expected error message to appear somewhere in log output")
}

func TestLogInfo(t *testing.T) {
	out := bytes.Buffer{}
	Log.SetLogOut(&out)

	testManifest := getTestPodManifest(t)
	pod := PodFromManifestId(testManifest.ID())
	message := "Pod did something good"
	pod.logInfo(message)

	output, err := ioutil.ReadAll(&out)
	Assert(t).IsNil(err, "Got an error reading the logging output")
	outputString := bytes.NewBuffer(output).String()
	Assert(t).Matches(outputString, ContainsString("hello"), "Expected 'hello' to appear somewhere in log output")
	Assert(t).Matches(outputString, ContainsString("Pod did something good"), "Expected error message to appear somewhere in log output")
}

func TestWriteManifestWillReturnOldManifestTempPath(t *testing.T) {
	existing := getTestPodManifest(t).(*manifest)
	updated := getUpdatedManifest(t).(*manifest)

	poddir, err := ioutil.TempDir("", "poddir")
	Assert(t).IsNil(err, "couldn't create tempdir")
	pod := NewPod(types.PodID("testPod"), poddir)

	// set the RunAs user to the user running the test, because when we
	// write files we need an owner. Unset them after the write so that the
	// manifests match the manifests on disk
	oldPath := func() string {
		currUser, err := user.Current()
		Assert(t).IsNil(err, "Could not get the current user")
		existing.RunAs = currUser.Username
		defer func() { existing.RunAs = "" }()
		updated.RunAs = currUser.Username
		defer func() { updated.RunAs = "" }()

		manifestContent, err := existing.Marshal()
		Assert(t).IsNil(err, "couldn't get manifest bytes")
		err = ioutil.WriteFile(pod.currentPodManifestPath(), manifestContent, 0744)
		Assert(t).IsNil(err, "should have written current manifest")

		oldPath, err := pod.WriteCurrentManifest(updated)
		Assert(t).IsNil(err, "should have written the current manifest and linked the old one")
		return oldPath
	}()
	existing.RunAs = ""
	updated.RunAs = ""

	writtenOld, err := ManifestFromPath(oldPath)
	Assert(t).IsNil(err, "should have written a manifest to the old path")
	manifestMustEqual(existing, writtenOld, t)

	writtenCurrent, err := pod.CurrentManifest()
	Assert(t).IsNil(err, "the manifest was not written properly")
	manifestMustEqual(updated, writtenCurrent, t)
}

func TestBuildRunitServices(t *testing.T) {
	fakeSB := runit.FakeServiceBuilder()
	defer fakeSB.Cleanup()
	serviceBuilder := &fakeSB.ServiceBuilder

	pod := Pod{
		Id:             "testPod",
		path:           "/data/pods/testPod",
		ServiceBuilder: serviceBuilder,
		LogExec:        defaultLogExec,
	}
	hl, sb := hoist.FakeHoistLaunchableForDir("multiple_script_test_hoist_launchable")
	defer hoist.CleanupFakeLaunchable(hl, sb)
	hl.RunAs = "testPod"
	executables, err := hl.Executables(serviceBuilder)
	outFilePath := filepath.Join(serviceBuilder.ConfigRoot, "testPod.yaml")

	Assert(t).IsNil(err, "Got an unexpected error when attempting to start runit services")
	testManifest := &manifest{RestartPolicy: runit.RestartPolicyAlways}
	pod.buildRunitServices([]launch.Launchable{hl.If()}, testManifest)

	f, err := os.Open(outFilePath)
	defer f.Close()
	bytes, err := ioutil.ReadAll(f)
	Assert(t).IsNil(err, "Got an unexpected error reading the servicebuilder yaml file")

	expectedMap := map[string]runit.ServiceTemplate{
		executables[0].Service.Name: runit.ServiceTemplate{
			Run: executables[0].Exec,
			Log: defaultLogExec,
		},
		executables[1].Service.Name: runit.ServiceTemplate{
			Run: executables[1].Exec,
			Log: defaultLogExec,
		},
	}
	expected, err := yaml.Marshal(expectedMap)
	Assert(t).IsNil(err, "Got error marshalling expected map to yaml")

	Assert(t).AreEqual(string(bytes), string(expected), "Servicebuilder yaml file didn't have expected contents")
}

func TestUninstall(t *testing.T) {
	fakeSB := runit.FakeServiceBuilder()
	defer fakeSB.Cleanup()
	serviceBuilder := &fakeSB.ServiceBuilder

	testPodDir, err := ioutil.TempDir("", "testPodDir")
	Assert(t).IsNil(err, "Got an unexpected error creating a temp directory")
	pod := Pod{
		Id:             "testPod",
		path:           testPodDir,
		ServiceBuilder: serviceBuilder,
	}
	manifest := getTestPodManifest(t)
	manifestContent, err := manifest.Marshal()
	Assert(t).IsNil(err, "couldn't get manifest bytes")
	err = ioutil.WriteFile(pod.currentPodManifestPath(), manifestContent, 0744)
	Assert(t).IsNil(err, "should have written current manifest")

	serviceBuilderFilePath := filepath.Join(serviceBuilder.ConfigRoot, "testPod.yaml")
	err = ioutil.WriteFile(serviceBuilderFilePath, []byte("stuff"), 0744)
	Assert(t).IsNil(err, "Error writing fake servicebuilder file")

	err = pod.Uninstall()
	Assert(t).IsNil(err, "Error uninstalling pod")
	_, err = os.Stat(serviceBuilderFilePath)
	Assert(t).IsTrue(os.IsNotExist(err), "Expected file to not exist after uninstall")
	_, err = os.Stat(pod.currentPodManifestPath())
	Assert(t).IsTrue(os.IsNotExist(err), "Expected file to not exist after uninstall")
}

func manifestMustEqual(expected, actual Manifest, t *testing.T) {
	actualSha, err := actual.SHA()
	Assert(t).IsNil(err, "should have gotten SHA from old manifest")
	expectedSha, err := expected.SHA()
	Assert(t).IsNil(err, "should have gotten SHA from known old manifest")
	manifestBytes, err := expected.Marshal()
	Assert(t).IsNil(err, "should have gotten bytes from manifest")
	actualBytes, err := actual.Marshal()
	Assert(t).IsNil(err, "should have gotten bytes from writtenOld")
	Assert(t).AreEqual(expectedSha, actualSha, fmt.Sprintf("known: \n\n%s\n\nactual:\n\n%s\n", string(manifestBytes), string(actualBytes)))
}

func ContainsString(test string) func(interface{}) bool {
	return func(subject interface{}) bool {
		if subjectString, ok := subject.(string); ok {
			return strings.Contains(subjectString, test)
		} else {
			return false
		}
	}
}
