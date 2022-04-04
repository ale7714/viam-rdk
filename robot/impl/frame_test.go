package robotimpl_test

import (
	"context"
	"testing"

	"github.com/edaniels/golog"
	"github.com/golang/geo/r3"
	"github.com/pkg/errors"
	"go.viam.com/test"

	"go.viam.com/rdk/config"
	"go.viam.com/rdk/referenceframe"
	robotimpl "go.viam.com/rdk/robot/impl"
)

var blankPos map[string][]referenceframe.Input

func TestFrameSystemFromConfig(t *testing.T) {
	// use impl/data/fake.json as config input
	emptyIn := []referenceframe.Input{}
	logger := golog.NewTestLogger(t)
	cfg, err := config.Read(context.Background(), "data/fake.json", logger)
	test.That(t, err, test.ShouldBeNil)

	r, err := robotimpl.New(context.Background(), cfg, logger)
	test.That(t, err, test.ShouldBeNil)
	defer r.Close(context.Background())

	// use fake registrations to have a FrameSystem return
	fs, err := r.FrameSystem(context.Background(), "test", "")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(fs.FrameNames()), test.ShouldEqual, 8) // 4 frames defined, 8 frames when including the offset

	// see if all frames are present and if their frames are correct
	test.That(t, fs.GetFrame("world"), test.ShouldNotBeNil)

	t.Log("pieceArm")
	test.That(t, fs.GetFrame("pieceArm"), test.ShouldNotBeNil)
	pose, err := fs.GetFrame("pieceArm").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{500, 0, 300})

	t.Log("pieceArm_offset")
	test.That(t, fs.GetFrame("pieceArm_offset"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("pieceArm_offset").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{500, 500, 1000})

	t.Log("pieceGripper")
	test.That(t, fs.GetFrame("pieceGripper"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("pieceGripper").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{0, 0, 200})

	t.Log("pieceGripper_offset")
	test.That(t, fs.GetFrame("pieceGripper_offset"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("pieceGripper_offset").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{0, 0, 0})

	t.Log("gps2")
	test.That(t, fs.GetFrame("gps2"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("gps2").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{0, 0, 0})

	t.Log("gps2_offset")
	test.That(t, fs.GetFrame("gps2_offset"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("gps2_offset").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{0, 0, 0})

	t.Log("cameraOver")
	test.That(t, fs.GetFrame("cameraOver"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("cameraOver").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{0, 0, 0})

	t.Log("cameraOver_offset")
	test.That(t, fs.GetFrame("cameraOver_offset"), test.ShouldNotBeNil)
	pose, err = fs.GetFrame("cameraOver_offset").Transform(emptyIn)
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, pose.Point(), r3.Vector{2000, 500, 1300})

	t.Log("gps1")
	test.That(t, fs.GetFrame("gps1"), test.ShouldBeNil) // gps1 is not registered

	// There is a point at (1500, 500, 1300) in the world referenceframe. See if it transforms correctly in each referenceframe.
	worldPt := r3.Vector{1500, 500, 1300}
	armPt := r3.Vector{0, 0, 500}
	transformPoint, err := fs.TransformPoint(blankPos, worldPt, referenceframe.World, "pieceArm")
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, transformPoint, armPt)

	sensorPt := r3.Vector{0, 0, 500}
	transformPoint, err = fs.TransformPoint(blankPos, worldPt, referenceframe.World, "gps2")
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, transformPoint, sensorPt)

	gripperPt := r3.Vector{0, 0, 300}
	transformPoint, err = fs.TransformPoint(blankPos, worldPt, referenceframe.World, "pieceGripper")
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, transformPoint, gripperPt)

	cameraPt := r3.Vector{500, 0, 0}
	transformPoint, err = fs.TransformPoint(blankPos, worldPt, referenceframe.World, "cameraOver")
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, transformPoint, cameraPt)

	// go from camera point to gripper point
	transformPoint, err = fs.TransformPoint(blankPos, cameraPt, "cameraOver", "pieceGripper")
	test.That(t, err, test.ShouldBeNil)
	pointAlmostEqual(t, transformPoint, gripperPt)
}

// All of these config files should fail.
func TestWrongFrameSystems(t *testing.T) {
	// use impl/data/fake_wrongconfig*.json as config input
	logger := golog.NewTestLogger(t)
	cfg, err := config.Read(context.Background(), "data/fake_wrongconfig2.json", logger) // no world node
	test.That(t, err, test.ShouldBeNil)
	_, err = robotimpl.New(context.Background(), cfg, logger)
	test.That(t,
		err, test.ShouldBeError, errors.New("there are no frames that connect to a 'world' node. Root node must be named 'world'"))

	cfg, err = config.Read(context.Background(), "data/fake_wrongconfig3.json", logger) // one of the nodes was given the name world
	test.That(t, err, test.ShouldBeNil)
	_, err = robotimpl.New(context.Background(), cfg, logger)
	test.That(t, err, test.ShouldBeError, errors.New("cannot have more than one frame with name world"))

	cfg, err = config.Read(context.Background(), "data/fake_wrongconfig4.json", logger) // the parent field was left empty for a component
	test.That(t, err, test.ShouldBeNil)
	_, err = robotimpl.New(context.Background(), cfg, logger)
	test.That(t, err, test.ShouldBeError, errors.New("parent field in frame config for part \"cameraOver\" is empty"))
}

func pointAlmostEqual(t *testing.T, from, to r3.Vector) {
	t.Helper()
	test.That(t, from.X, test.ShouldAlmostEqual, to.X)
	test.That(t, from.Y, test.ShouldAlmostEqual, to.Y)
	test.That(t, from.Z, test.ShouldAlmostEqual, to.Z)
}
