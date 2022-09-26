package builtin_test

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edaniels/golog"
	"github.com/pkg/errors"
	"go.viam.com/test"
	"go.viam.com/utils"
	"gopkg.in/yaml.v2"

	"go.viam.com/rdk/rimage/transform"
	"go.viam.com/rdk/services/slam/builtin"
)

const (
	yamlFilePrefixBytes = "%YAML:1.0\n"
	slamTimeFormat      = "2006-01-02T15_04_05.0000"
)

// function to search a SLAM data dir for a .yaml file. returns the timestamp and filepath.
func findLastYAML(folderName string) (string, string, error) {
	root := filepath.Join(folderName, "config")
	yamlExt := ".yaml"
	yamlTimestamp := time.Time{}
	var yamlPath string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if !entry.IsDir() && filepath.Ext(path) == yamlExt {
			// check if the file uses our format and grab timestamp if it does
			timestampLoc := strings.Index(entry.Name(), "_data_") + len("_data_")
			if timestampLoc != -1+len("_data_") {
				timestamp, err := time.Parse(slamTimeFormat, entry.Name()[timestampLoc:strings.Index(entry.Name(), yamlExt)])
				if err != nil {
					return errors.Wrap(err, "Unable to parse yaml")
				}
				if timestamp.After(yamlTimestamp) {
					yamlTimestamp = timestamp
					yamlPath = path
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if yamlTimestamp.IsZero() {
		return "", "", errors.New("No yaml file found")
	}
	return yamlTimestamp.UTC().Format(slamTimeFormat), yamlPath, nil
}

func TestOrbslamYAMLNew(t *testing.T) {
	name, err := createTempFolderArchitecture()
	test.That(t, err, test.ShouldBeNil)

	createFakeSLAMLibraries()
	dataRateMs := 200
	attrCfgGood := &builtin.AttrConfig{
		Algorithm: "fake_orbslamv3",
		Sensors:   []string{"good_camera"},
		ConfigParams: map[string]string{
			"mode":              "mono",
			"orb_n_features":    "1000",
			"orb_scale_factor":  "1.2",
			"orb_n_levels":      "8",
			"orb_n_ini_th_fast": "20",
			"orb_n_min_th_fast": "7",
		},
		DataDirectory: name,
		DataRateMs:    dataRateMs,
		Port:          "localhost:4445",
	}
	attrCfgBadCam := &builtin.AttrConfig{
		Algorithm: "fake_orbslamv3",
		Sensors:   []string{"bad_camera_intrinsics"},
		ConfigParams: map[string]string{
			"mode":              "mono",
			"orb_n_features":    "1000",
			"orb_scale_factor":  "1.2",
			"orb_n_levels":      "8",
			"orb_n_ini_th_fast": "20",
			"orb_n_min_th_fast": "7",
		},
		DataDirectory: name,
		DataRateMs:    dataRateMs,
		Port:          "localhost:4445",
	}
	var fakeMap string
	var fakeMapTimestamp string
	t.Run("New orbslamv3 service with good camera and defined params", func(t *testing.T) {
		// Create slam service
		logger := golog.NewTestLogger(t)
		grpcServer := setupTestGRPCServer(attrCfgGood.Port)
		svc, err := createSLAMService(t, attrCfgGood, logger, false, true)
		test.That(t, err, test.ShouldBeNil)

		grpcServer.Stop()
		test.That(t, utils.TryClose(context.Background(), svc), test.ShouldBeNil)

		yamlFileTimeStampGood, yamlFilePathGood, err := findLastYAML(name)

		fakeMapTimestamp = yamlFileTimeStampGood
		test.That(t, err, test.ShouldBeNil)

		yamlDataAll, err := os.ReadFile(yamlFilePathGood)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, yamlDataAll[:len(yamlFilePrefixBytes)], test.ShouldResemble, []byte(yamlFilePrefixBytes))

		yamlData := bytes.Replace(yamlDataAll, []byte(yamlFilePrefixBytes), []byte(""), 1)
		orbslam := builtin.ORBsettings{}
		err = yaml.Unmarshal(yamlData, &orbslam)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, orbslam.Width, test.ShouldEqual, 1280)
		test.That(t, orbslam.NLevels, test.ShouldEqual, 8)
		test.That(t, orbslam.ScaleFactor, test.ShouldEqual, 1.2)
		test.That(t, orbslam.LoadMapLoc, test.ShouldEqual, "")

		//save a fake map for the next map using the previous timestamp
		fakeMap = filepath.Join(name, "map", attrCfgGood.Sensors[0]+"_data_"+yamlFileTimeStampGood)
		test.That(t, orbslam.SaveMapLoc, test.ShouldEqual, fakeMap)
		outfile, err := os.Create(fakeMap + ".osa")
		test.That(t, err, test.ShouldBeNil)
		err = outfile.Close()
		test.That(t, err, test.ShouldBeNil)
	})

	t.Run("New orbslamv3 service with previous map and good camera", func(t *testing.T) {
		// Create slam service
		logger := golog.NewTestLogger(t)
		grpcServer := setupTestGRPCServer(attrCfgGood.Port)
		svc, err := createSLAMService(t, attrCfgGood, logger, false, true)
		test.That(t, err, test.ShouldBeNil)

		grpcServer.Stop()
		test.That(t, utils.TryClose(context.Background(), svc), test.ShouldBeNil)

		// Should have the same name due to map being found
		yamlFileTimeStampGood, yamlFilePathGood, err := findLastYAML(name)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, yamlFileTimeStampGood, test.ShouldEqual, fakeMapTimestamp)

		// check if map was specified to load
		yamlDataAll, err := os.ReadFile(yamlFilePathGood)
		test.That(t, err, test.ShouldBeNil)
		yamlData := bytes.Replace(yamlDataAll, []byte(yamlFilePrefixBytes), []byte(""), 1)
		orbslam := builtin.ORBsettings{}
		err = yaml.Unmarshal(yamlData, &orbslam)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, orbslam.LoadMapLoc, test.ShouldEqual, fakeMap)

		// compare timestamps, saveTimeStamp should be more recent than oldTimeStamp
		saveTimestampLoc := strings.Index(orbslam.SaveMapLoc, "_data_") + len("_data_")
		saveTimeStamp, err := time.Parse(slamTimeFormat, orbslam.SaveMapLoc[saveTimestampLoc:])
		test.That(t, err, test.ShouldBeNil)
		oldTimeStamp, err := time.Parse(slamTimeFormat, fakeMapTimestamp)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, saveTimeStamp.After(oldTimeStamp), test.ShouldBeTrue)
	})

	t.Run("New orbslamv3 service with camera that errors from bad intrinsics", func(t *testing.T) {
		// Create slam service
		logger := golog.NewTestLogger(t)
		_, err := createSLAMService(t, attrCfgBadCam, logger, false, false)

		test.That(t, err.Error(), test.ShouldContainSubstring,
			transform.NewNoIntrinsicsError(fmt.Sprintf("Invalid size (%#v, %#v)", 0, 0)).Error())
	})

	t.Run("New orbslamv3 service with camera that errors from bad orbslam params", func(t *testing.T) {
		// check if a param is empty
		attrCfgBadParam1 := &builtin.AttrConfig{
			Algorithm: "fake_orbslamv3",
			Sensors:   []string{"good_camera"},
			ConfigParams: map[string]string{
				"mode":              "mono",
				"orb_n_features":    "",
				"orb_scale_factor":  "1.2",
				"orb_n_levels":      "8",
				"orb_n_ini_th_fast": "20",
				"orb_n_min_th_fast": "7",
			},
			DataDirectory: name,
			DataRateMs:    dataRateMs,
			Port:          "localhost:4445",
		}
		// Create slam service
		logger := golog.NewTestLogger(t)
		_, err := createSLAMService(t, attrCfgBadParam1, logger, false, false)
		test.That(t, err.Error(), test.ShouldContainSubstring, "Parameter orb_n_features has an invalid definition")

		attrCfgBadParam2 := &builtin.AttrConfig{
			Algorithm: "fake_orbslamv3",
			Sensors:   []string{"good_camera"},
			ConfigParams: map[string]string{
				"mode":              "mono",
				"orb_n_features":    "1000",
				"orb_scale_factor":  "afhaf",
				"orb_n_levels":      "8",
				"orb_n_ini_th_fast": "20",
				"orb_n_min_th_fast": "7",
			},
			DataDirectory: name,
			DataRateMs:    dataRateMs,
			Port:          "localhost:4445",
		}
		_, err = createSLAMService(t, attrCfgBadParam2, logger, false, false)

		test.That(t, err.Error(), test.ShouldContainSubstring, "Parameter orb_scale_factor has an invalid definition")
	})

	closeOutSLAMService(t, name)
}