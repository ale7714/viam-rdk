// Package robotimpl defines implementations of robot.Robot and robot.LocalRobot.
//
// It also provides a remote robot implementation that is aware that the robot.Robot
// it is working with is not on the same physical system.
package robotimpl

import (
	"context"
	"fmt"
	"sync"

	"github.com/edaniels/golog"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
	"go.viam.com/utils/pexec"

	// registers all components.
	_ "go.viam.com/rdk/component/register"
	"go.viam.com/rdk/config"

	// register vm engines.
	_ "go.viam.com/rdk/function/vm/engines/javascript"
	"go.viam.com/rdk/metadata/service"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/registry"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/robot"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/framesystem"

	// registers all services.
	_ "go.viam.com/rdk/services/register"
	"go.viam.com/rdk/services/sensors"
	"go.viam.com/rdk/services/status"
	"go.viam.com/rdk/services/web"
	"go.viam.com/rdk/utils"
)

var (
	_ = robot.LocalRobot(&localRobot{})

	// defaultSvc is a list of default robot services.
	defaultSvc = []resource.Name{sensors.Name, status.Name, web.Name, datamanager.Name}
)

// localRobot satisfies robot.LocalRobot and defers most
// logic to its manager.
type localRobot struct {
	mu      sync.Mutex
	manager *resourceManager
	config  *config.Config
	logger  golog.Logger
}

// RemoteByName returns a remote robot by name. If it does not exist
// nil is returned.
func (r *localRobot) RemoteByName(name string) (robot.Robot, bool) {
	return r.manager.RemoteByName(name)
}

// ResourceByName returns a resource by name. If it does not exist
// nil is returned.
func (r *localRobot) ResourceByName(name resource.Name) (interface{}, error) {
	return r.manager.ResourceByName(name)
}

// RemoteNames returns the name of all known remote robots.
func (r *localRobot) RemoteNames() []string {
	return r.manager.RemoteNames()
}

// FunctionNames returns the name of all known functions.
func (r *localRobot) FunctionNames() []string {
	return r.manager.FunctionNames()
}

// ResourceNames returns the name of all known resources.
func (r *localRobot) ResourceNames() []resource.Name {
	return r.manager.ResourceNames()
}

// ProcessManager returns the process manager for the robot.
func (r *localRobot) ProcessManager() pexec.ProcessManager {
	return r.manager.processManager
}

// Close attempts to cleanly close down all constituent parts of the robot.
func (r *localRobot) Close(ctx context.Context) error {
	return r.manager.Close(ctx)
}

// Config returns the config used to construct the robot. Only local resources are returned.
// This is allowed to be partial or empty.
func (r *localRobot) Config(ctx context.Context) (*config.Config, error) {
	cfgCpy := *r.config
	cfgCpy.Components = append([]config.Component{}, cfgCpy.Components...)

	return &cfgCpy, nil
}

// getRemoteConfig gets the parameters for the Remote.
func (r *localRobot) getRemoteConfig(remoteName string) (*config.Remote, error) {
	for _, rConf := range r.config.Remotes {
		if rConf.Name == remoteName {
			return &rConf, nil
		}
	}
	return nil, fmt.Errorf("cannot find Remote config with name %q", remoteName)
}

// FrameSystem returns the FrameSystem of the robot.
func (r *localRobot) FrameSystem(ctx context.Context, name, prefix string) (referenceframe.FrameSystem, error) {
	ctx, span := trace.StartSpan(ctx, "local-robot::FrameSystem")
	defer span.End()
	logger := r.Logger()
	// create the base reference frame system
	fsService, err := framesystem.FromRobot(r)
	if err != nil {
		return nil, err
	}
	parts, err := fsService.Config(ctx)
	if err != nil {
		return nil, err
	}
	// get frame parts for each of its remotes
	for remoteName, remote := range r.manager.remotes {
		remoteService, err := framesystem.FromRobot(remote)
		if err != nil {
			return nil, errors.Wrapf(err, "remote %s", remoteName)
		}
		remoteParts, err := remoteService.Config(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "remote %s", remoteName)
		}
		rConf, err := r.getRemoteConfig(remoteName)
		if err != nil {
			return nil, errors.Wrapf(err, "remote %s", remoteName)
		}
		if rConf.Frame == nil { // skip over remote if it has no frame info
			logger.Debugf("remote %s has no frame config info, skipping", remoteName)
			continue
		}
		remoteParts = renameRemoteParts(remoteParts, rConf)
		parts = append(parts, remoteParts...)
	}
	baseFrameSys, err := framesystem.NewFrameSystemFromParts(name, "", parts, logger)
	if err != nil {
		return nil, err
	}
	logger.Debugf("final frame system  %q has frames %v", baseFrameSys.Name(), baseFrameSys.FrameNames())
	return baseFrameSys, nil
}

func renameRemoteParts(remoteParts []*config.FrameSystemPart, remoteConf *config.Remote) []*config.FrameSystemPart {
	connectionName := remoteConf.Name + "_" + referenceframe.World
	for _, p := range remoteParts {
		if p.FrameConfig.Parent == referenceframe.World { // rename World of remote parts
			p.FrameConfig.Parent = connectionName
		}
		if remoteConf.Prefix { // rename each non-world part with prefix
			p.Name = remoteConf.Name + "." + p.Name
			if p.FrameConfig.Parent != connectionName {
				p.FrameConfig.Parent = remoteConf.Name + "." + p.FrameConfig.Parent
			}
		}
	}
	// build the frame system part that connects remote world to base world
	connection := &config.FrameSystemPart{
		Name:        connectionName,
		FrameConfig: remoteConf.Frame,
	}
	remoteParts = append(remoteParts, connection)
	return remoteParts
}

// Logger returns the logger the robot is using.
func (r *localRobot) Logger() golog.Logger {
	return r.logger
}

// New returns a new robot with parts sourced from the given config.
func New(ctx context.Context, cfg *config.Config, logger golog.Logger) (robot.LocalRobot, error) {
	r := &localRobot{
		manager: newResourceManager(
			resourceManagerOptions{
				debug:              cfg.Debug,
				fromCommand:        cfg.FromCommand,
				allowInsecureCreds: cfg.AllowInsecureCreds,
				tlsConfig:          cfg.Network.TLSConfig,
			},
			logger,
		),
		logger: logger,
	}

	var successful bool
	defer func() {
		if !successful {
			if err := r.Close(context.Background()); err != nil {
				logger.Errorw("failed to close robot down after startup failure", "error", err)
			}
		}
	}()
	r.config = cfg

	if err := r.manager.processConfig(ctx, cfg, r, logger); err != nil {
		return nil, err
	}

	// default services
	for _, name := range defaultSvc {
		cfg := config.Service{Type: config.ServiceType(name.ResourceSubtype)}
		svc, err := r.newService(ctx, cfg)
		if err != nil {
			return nil, err
		}
		r.manager.addResource(name, svc)
	}

	// update default services - done here so that all resources have been created and can be addressed.
	if err := r.updateDefaultServices(ctx); err != nil {
		return nil, err
	}

	// if metadata exists, update it
	if svc := service.ContextService(ctx); svc != nil {
		if err := r.UpdateMetadata(svc); err != nil {
			return nil, err
		}
	}
	successful = true
	return r, nil
}

func (r *localRobot) newService(ctx context.Context, config config.Service) (interface{}, error) {
	rName := config.ResourceName()
	f := registry.ServiceLookup(rName.Subtype)
	if f == nil {
		return nil, errors.Errorf("unknown service type: %s", rName.Subtype)
	}
	return f.Constructor(ctx, r, config, r.logger)
}

func (r *localRobot) newResource(ctx context.Context, config config.Component) (interface{}, error) {
	rName := config.ResourceName()
	f := registry.ComponentLookup(rName.Subtype, config.Model)
	if f == nil {
		return nil, errors.Errorf("unknown component subtype: %s and/or model: %s", rName.Subtype, config.Model)
	}
	newResource, err := f.Constructor(ctx, r, config, r.logger)
	if err != nil {
		return nil, err
	}
	c := registry.ResourceSubtypeLookup(rName.Subtype)
	if c == nil || c.Reconfigurable == nil {
		return newResource, nil
	}
	return c.Reconfigurable(newResource)
}

// ConfigUpdateable is implemented when component/service of a robot should be updated with the config.
type ConfigUpdateable interface {
	// Update updates the resource
	Update(context.Context, config.Service) error
}

// Get the config associated with the service resource.
func getServiceConfig(cfg *config.Config, name resource.Name) (config.Service, error) {
	for _, c := range cfg.Services {
		if c.ResourceName() == name {
			return c, nil
		}
	}
	return config.Service{}, errors.Errorf("could not find service config of subtype %s", name.Subtype.String())
}

func (r *localRobot) updateDefaultServices(ctx context.Context) error {
	// grab all resources
	resources := map[resource.Name]interface{}{}
	for _, n := range r.ResourceNames() {
		// TODO(RDK-119) if not found, could mean a name clash or a remote service
		res, err := r.ResourceByName(n)
		if err != nil {
			r.logger.Debugf("not found while grabbing all resources during default svc refresh: %w", err)
		}
		resources[n] = res
	}
	for _, name := range defaultSvc {
		svc, err := r.ResourceByName(name)
		if err != nil {
			return utils.NewResourceNotFoundError(name)
		}
		if updateable, ok := svc.(resource.Updateable); ok {
			if err := updateable.Update(ctx, resources); err != nil {
				return err
			}
		}
		if configUpdateable, ok := svc.(ConfigUpdateable); ok {
			serviceConfig, err := getServiceConfig(r.config, name)
			if err == nil {
				if err := configUpdateable.Update(ctx, serviceConfig); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Refresh does nothing for now.
func (r *localRobot) Refresh(ctx context.Context) error {
	return nil
}

// UpdateMetadata updates metadata service using the currently registered parts of the robot.
func (r *localRobot) UpdateMetadata(svc service.Metadata) error {
	var resources []resource.Name

	metadata := resource.NameFromSubtype(service.Subtype, "")
	resources = append(resources, metadata)

	for _, name := range r.FunctionNames() {
		res := resource.NewName(
			resource.ResourceNamespaceRDK,
			resource.ResourceTypeFunction,
			resource.ResourceSubtypeFunction,
			name,
		)
		resources = append(resources, res)
	}
	for _, name := range r.RemoteNames() {
		res := resource.NewName(
			resource.ResourceNamespaceRDK,
			resource.ResourceTypeComponent,
			resource.ResourceSubtypeRemote,
			name,
		)
		resources = append(resources, res)
	}

	for _, n := range r.ResourceNames() {
		// skip web so it doesn't show up over grpc
		if n == web.Name {
			continue
		}
		resources = append(resources, n)
	}
	return svc.Replace(resources)
}
