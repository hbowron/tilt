package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/session/filesync"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	fsutiltypes "github.com/tonistiigi/fsutil/types"
	ktypes "k8s.io/apimachinery/pkg/types"

	"github.com/tilt-dev/tilt/internal/container"
	"github.com/tilt-dev/tilt/internal/docker"
	"github.com/tilt-dev/tilt/internal/dockerfile"
	"github.com/tilt-dev/tilt/internal/k8s"
	"github.com/tilt-dev/tilt/pkg/apis/core/v1alpha1"
	"github.com/tilt-dev/tilt/pkg/logger"
	"github.com/tilt-dev/tilt/pkg/model"
)

type DockerBuilder struct {
	dCli docker.Client

	// A set of extra labels to attach to all builds
	// created by this image builder.
	//
	// By default, all builds are labeled with a build mode.
	extraLabels dockerfile.Labels
}

// Describes how a docker instance connects to kubernetes instances.
type DockerKubeConnection interface {
	// Returns whether this docker builder is going to build to the given kubernetes context.
	WillBuildToKubeContext(kctx k8s.KubeContext) bool
}

func NewDockerBuilder(dCli docker.Client, extraLabels dockerfile.Labels) *DockerBuilder {
	return &DockerBuilder{
		dCli:        dCli,
		extraLabels: extraLabels,
	}
}

func (d *DockerBuilder) WillBuildToKubeContext(kctx k8s.KubeContext) bool {
	return d.dCli.Env().WillBuildToKubeContext(kctx)
}

func (d *DockerBuilder) DumpImageDeployRef(ctx context.Context, ref string) (reference.NamedTagged, error) {
	refParsed, err := container.ParseNamed(ref)
	if err != nil {
		return nil, errors.Wrap(err, "DumpImageDeployRef")
	}

	data, _, err := d.dCli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return nil, errors.Wrap(err, "DumpImageDeployRef")
	}
	dig := digest.Digest(data.ID)

	tag, err := digestAsTag(dig)
	if err != nil {
		return nil, errors.Wrap(err, "DumpImageDeployRef")
	}

	tagged, err := reference.WithTag(refParsed, tag)
	if err != nil {
		return nil, errors.Wrap(err, "DumpImageDeployRef")
	}

	return tagged, nil
}

// Tag the digest with the given name and wm-tilt tag.
func (d *DockerBuilder) TagRefs(ctx context.Context, refs container.RefSet, dig digest.Digest) (container.TaggedRefs, error) {
	tag, err := digestAsTag(dig)
	if err != nil {
		return container.TaggedRefs{}, errors.Wrap(err, "TagImage")
	}

	tagged, err := refs.AddTagSuffix(tag)
	if err != nil {
		return container.TaggedRefs{}, errors.Wrap(err, "TagImage")
	}

	// Docker client only needs to care about the localImage
	err = d.dCli.ImageTag(ctx, dig.String(), tagged.LocalRef.String())
	if err != nil {
		return container.TaggedRefs{}, errors.Wrap(err, "TagImage#ImageTag")
	}

	return tagged, nil
}

// Push the specified ref up to the docker registry specified in the name.
//
// TODO(nick) In the future, I would like us to be smarter about checking if the kubernetes cluster
// we're running in has access to the given registry. And if it doesn't, we should either emit an
// error, or push to a registry that kubernetes does have access to (e.g., a local registry).
func (d *DockerBuilder) PushImage(ctx context.Context, ref reference.NamedTagged) error {
	l := logger.Get(ctx)

	imagePushResponse, err := d.dCli.ImagePush(ctx, ref)
	if err != nil {
		return errors.Wrap(err, "PushImage#ImagePush")
	}

	defer func() {
		err := imagePushResponse.Close()
		if err != nil {
			l.Infof("unable to close imagePushResponse: %s", err)
		}
	}()

	_, _, err = readDockerOutput(ctx, imagePushResponse)
	if err != nil {
		return errors.Wrapf(err, "pushing image %q", ref.Name())
	}

	return nil
}

func (d *DockerBuilder) ImageExists(ctx context.Context, ref reference.NamedTagged) (bool, error) {
	_, _, err := d.dCli.ImageInspectWithRaw(ctx, ref.String())
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "error checking if %s exists", ref.String())
	}
	return true, nil
}

func (d *DockerBuilder) BuildImage(ctx context.Context, ps *PipelineState, refs container.RefSet,
	spec v1alpha1.DockerImageSpec,
	cluster *v1alpha1.Cluster,
	imageMaps map[ktypes.NamespacedName]*v1alpha1.ImageMap,
	filter model.PathMatcher) (container.TaggedRefs, []v1alpha1.DockerImageStageStatus, error) {
	spec = InjectClusterPlatform(spec, cluster)
	spec, err := InjectImageDependencies(spec, imageMaps)
	if err != nil {
		return container.TaggedRefs{}, nil, err
	}

	platformSuffix := ""
	if spec.Platform != "" {
		platformSuffix = fmt.Sprintf(" for platform %s", spec.Platform)
	}
	logger.Get(ctx).Infof("Building Dockerfile%s:\n%s\n", platformSuffix, indent(spec.DockerfileContents, "  "))

	ps.StartBuildStep(ctx, "Building image")
	allowBuildkit := true
	ctx = ps.AttachLogger(ctx)
	digest, stages, err := d.buildToDigest(ctx, spec, filter, allowBuildkit)
	if err != nil {
		isMysteriousCorruption := strings.Contains(err.Error(), "failed precondition") &&
			strings.Contains(err.Error(), "failed commit on ref")
		if isMysteriousCorruption {
			// We've seen weird corruption issues on buildkit
			// that look like
			//
			// Build Failed: ImageBuild: failed to create LLB definition:
			// failed commit on ref "unknown-sha256:b72fa303a3a5fbf52c723bfcfb93948bb53b3d7e8d22418e9d171a27ad7dcd84":
			// "unknown-sha256:b72fa303a3a5fbf52c723bfcfb93948bb53b3d7e8d22418e9d171a27ad7dcd84"
			// failed size validation: 80941 != 80929: failed precondition
			//
			// Build Failed: ImageBuild: failed to load cache key: failed commit on
			// ref
			// "unknown-sha256:d8ad5905555e3af3fa9122515f2b3d4762d4e8734b7ed12f1271bcdee3541267":
			// unexpected commit size 69764, expected 76810: failed precondition
			//
			// If this happens, just try again without buildkit.
			allowBuildkit = false
			logger.Get(ctx).Infof("Detected Buildkit corruption. Rebuilding without Buildkit")
			digest, stages, err = d.buildToDigest(ctx, spec, filter, allowBuildkit)
		}

		if err != nil {
			return container.TaggedRefs{}, stages, err
		}
	}

	tagged, err := d.TagRefs(ctx, refs, digest)
	if err != nil {
		return container.TaggedRefs{}, stages, errors.Wrap(err, "docker tag")
	}

	return tagged, stages, nil
}

// A helper function that builds the paths to the given docker image,
// then returns the output digest.
func (d *DockerBuilder) buildToDigest(ctx context.Context, spec v1alpha1.DockerImageSpec, filter model.PathMatcher, allowBuildkit bool) (digest.Digest, []v1alpha1.DockerImageStageStatus, error) {
	var contextReader io.Reader

	// Buildkit allows us to use a fs sync server instead of uploading up-front.
	useFSSync := allowBuildkit && d.dCli.BuilderVersion() == types.BuilderBuildKit
	if !useFSSync {
		pipeReader, pipeWriter := io.Pipe()
		w := NewProgressWriter(ctx, pipeWriter)
		w.Init()

		// TODO(nick): Express tarring as a build stage.
		go func(ctx context.Context) {
			paths := []PathMapping{
				{
					LocalPath:     spec.Context,
					ContainerPath: "/",
				},
			}
			err := tarContextAndUpdateDf(ctx, w, dockerfile.Dockerfile(spec.DockerfileContents), paths, filter)
			if err != nil {
				_ = pipeWriter.CloseWithError(err)
			} else {
				_ = pipeWriter.Close()
			}
			w.Close() // Print the final progress message
		}(ctx)

		contextReader = pipeReader
		defer func() {
			_ = pipeReader.Close()
		}()
	}

	options := Options(contextReader, spec)
	if useFSSync {
		dockerfileDir, err := writeTempDockerfile(spec.DockerfileContents)
		if err != nil {
			return "", nil, err
		}
		options.SyncedDirs = toSyncedDirs(spec.Context, dockerfileDir, filter)
		options.Dockerfile = DockerfileName

		defer func() {
			_ = os.RemoveAll(dockerfileDir)
		}()
	}
	if !allowBuildkit {
		options.ForceLegacyBuilder = true
	}
	imageBuildResponse, err := d.dCli.ImageBuild(
		ctx,
		contextReader,
		options,
	)
	if err != nil {
		return "", nil, err
	}

	defer func() {
		err := imageBuildResponse.Body.Close()
		if err != nil {
			logger.Get(ctx).Infof("unable to close imageBuildResponse: %s", err)
		}
	}()

	return d.getDigestFromBuildOutput(ctx, imageBuildResponse.Body)
}

func (d *DockerBuilder) getDigestFromBuildOutput(ctx context.Context, reader io.Reader) (digest.Digest, []v1alpha1.DockerImageStageStatus, error) {
	result, stageStatuses, err := readDockerOutput(ctx, reader)
	if err != nil {
		return "", stageStatuses, errors.Wrap(err, "ImageBuild")
	}

	digest, err := d.getDigestFromDockerOutput(ctx, result)
	if err != nil {
		return "", stageStatuses, errors.Wrap(err, "getDigestFromBuildOutput")
	}

	return digest, stageStatuses, nil
}

var dockerBuildCleanupRexes = []*regexp.Regexp{
	// the "runc did not determinate sucessfully" just seems redundant on top of "executor failed running"
	// nolint
	regexp.MustCompile("(executor failed running.*): runc did not terminate sucessfully"), // sucessfully (sic)
	// when a file is missing, it generates an error like "failed to compute cache key: foo.txt not found: not found"
	// most of that seems redundant and/or confusing
	regexp.MustCompile("failed to compute cache key: (.* not found): not found"),
	regexp.MustCompile("failed to compute cache key: (?:failed to walk [^ ]+): lstat (?:/.*buildkit-[^/]*/)?(.*: no such file or directory)"),
}

// buildkit emits errors that might be useful for people who are into buildkit internals, but aren't really
// at the optimal level for people who just wanna build something
// ideally we'll get buildkit to emit errors with more structure so that we don't have to rely on string manipulation,
// but to have impact via that route, we've got to get the change in and users have to upgrade to a version of docker
// that has that change. So let's clean errors up here until that's in a good place.
func cleanupDockerBuildError(err string) string {
	// this is pretty much always the same, and meaningless noise to most users
	ret := strings.TrimPrefix(err, "failed to solve with frontend dockerfile.v0: ")
	ret = strings.TrimPrefix(ret, "failed to solve with frontend gateway.v0: ")
	ret = strings.TrimPrefix(ret, "rpc error: code = Unknown desc = ")
	ret = strings.TrimPrefix(ret, "failed to build LLB: ")
	for _, re := range dockerBuildCleanupRexes {
		ret = re.ReplaceAllString(ret, "$1")
	}
	return ret
}

type dockerMessageID string

// Docker API commands stream back a sequence of JSON messages.
//
// The result of the command is in a JSON object with field "aux".
//
// Errors are reported in a JSON object with field "errorDetail"
//
// NOTE(nick): I haven't found a good document describing this protocol
// but you can find it implemented in Docker here:
// https://github.com/moby/moby/blob/1da7d2eebf0a7a60ce585f89a05cebf7f631019c/pkg/jsonmessage/jsonmessage.go#L139
func readDockerOutput(ctx context.Context, reader io.Reader) (dockerOutput, []v1alpha1.DockerImageStageStatus, error) {
	progressLastPrinted := make(map[dockerMessageID]time.Time)

	result := dockerOutput{}
	decoder := json.NewDecoder(reader)
	b := newBuildkitPrinter(logger.Get(ctx))

	for decoder.More() {
		message := jsonmessage.JSONMessage{}
		err := decoder.Decode(&message)
		if err != nil {
			return dockerOutput{}, b.toStageStatuses(), errors.Wrap(err, "decoding docker output")
		}

		if len(message.Stream) > 0 {
			msg := message.Stream

			builtDigestMatch := oldDigestRegexp.FindStringSubmatch(msg)
			if len(builtDigestMatch) >= 2 {
				// Old versions of docker (pre 1.30) didn't send down an aux message.
				result.shortDigest = builtDigestMatch[1]
			}

			logger.Get(ctx).Write(logger.InfoLvl, []byte(msg))
		}

		if message.ErrorMessage != "" {
			return dockerOutput{}, b.toStageStatuses(), errors.New(cleanupDockerBuildError(message.ErrorMessage))
		}

		if message.Error != nil {
			return dockerOutput{}, b.toStageStatuses(), errors.New(cleanupDockerBuildError(message.Error.Message))
		}

		id := dockerMessageID(message.ID)
		if id != "" && message.Progress != nil {
			// Add a small 2-second backoff so that we don't overwhelm the logstore.
			lastPrinted, hasBeenPrinted := progressLastPrinted[id]
			shouldPrint := !hasBeenPrinted ||
				message.Progress.Current == message.Progress.Total ||
				time.Since(lastPrinted) > 2*time.Second
			shouldSkip := message.Progress.Current == 0 &&
				(message.Status == "Waiting" || message.Status == "Preparing")
			if shouldPrint && !shouldSkip {
				fields := logger.Fields{logger.FieldNameProgressID: message.ID}
				if message.Progress.Current == message.Progress.Total {
					fields[logger.FieldNameProgressMustPrint] = "1"
				}
				logger.Get(ctx).WithFields(fields).
					Infof("%s: %s %s", id, message.Status, message.Progress.String())
				progressLastPrinted[id] = time.Now()
			}
		}

		if messageIsFromBuildkit(message) {
			err := toBuildkitStatus(message.Aux, b)
			if err != nil {
				return dockerOutput{}, b.toStageStatuses(), err
			}
		}

		if message.Aux != nil && !messageIsFromBuildkit(message) {
			result.aux = message.Aux
		}
	}

	if ctx.Err() != nil {
		return dockerOutput{}, b.toStageStatuses(), ctx.Err()
	}
	return result, b.toStageStatuses(), nil
}

func toBuildkitStatus(aux *json.RawMessage, b *buildkitPrinter) error {
	var resp controlapi.StatusResponse
	var dt []byte
	// ignoring all messages that are not understood
	if err := json.Unmarshal(*aux, &dt); err != nil {
		return err
	}
	if err := (&resp).Unmarshal(dt); err != nil {
		return err
	}
	return b.parseAndPrint(toVertexes(resp))
}

func toVertexes(resp controlapi.StatusResponse) ([]*vertex, []*vertexLog, []*vertexStatus) {
	vertexes := []*vertex{}
	logs := []*vertexLog{}
	statuses := []*vertexStatus{}

	for _, v := range resp.Vertexes {
		duration := time.Duration(0)
		started := v.Started != nil
		completed := v.Completed != nil
		if started && completed {
			duration = (*v.Completed).Sub((*v.Started))
		}
		vertexes = append(vertexes, &vertex{
			digest:        v.Digest,
			name:          v.Name,
			error:         v.Error,
			started:       started,
			completed:     completed,
			cached:        v.Cached,
			duration:      duration,
			startedTime:   v.Started,
			completedTime: v.Completed,
		})

	}
	for _, v := range resp.Logs {
		logs = append(logs, &vertexLog{
			vertex: v.Vertex,
			msg:    v.Msg,
		})
	}
	for _, s := range resp.Statuses {
		statuses = append(statuses, &vertexStatus{
			vertex:    s.Vertex,
			id:        s.ID,
			total:     s.Total,
			current:   s.Current,
			timestamp: s.Timestamp,
		})
	}
	return vertexes, logs, statuses
}

func messageIsFromBuildkit(msg jsonmessage.JSONMessage) bool {
	return msg.ID == "moby.buildkit.trace"
}

func (d *DockerBuilder) getDigestFromDockerOutput(ctx context.Context, output dockerOutput) (digest.Digest, error) {
	if output.aux != nil {
		return getDigestFromAux(*output.aux)
	}

	if output.shortDigest != "" {
		data, _, err := d.dCli.ImageInspectWithRaw(ctx, output.shortDigest)
		if err != nil {
			return "", err
		}
		return digest.Digest(data.ID), nil
	}

	return "", fmt.Errorf("Docker is not responding. Maybe Docker is out of disk space? Try running `docker system prune`")
}

func getDigestFromAux(aux json.RawMessage) (digest.Digest, error) {
	digestMap := make(map[string]string)
	err := json.Unmarshal(aux, &digestMap)
	if err != nil {
		return "", errors.Wrap(err, "getDigestFromAux")
	}

	id, ok := digestMap["ID"]
	if !ok {
		return "", fmt.Errorf("getDigestFromAux: ID not found")
	}
	return digest.Digest(id), nil
}

func digestAsTag(d digest.Digest) (string, error) {
	str := d.Encoded()
	if len(str) < 16 {
		return "", fmt.Errorf("digest too short: %s", str)
	}
	return fmt.Sprintf("%s%s", ImageTagPrefix, str[:16]), nil
}

func digestMatchesRef(ref reference.NamedTagged, digest digest.Digest) bool {
	digestHash := digest.Encoded()
	tag := ref.Tag()
	if len(tag) <= len(ImageTagPrefix) {
		return false
	}

	tagHash := tag[len(ImageTagPrefix):]
	return strings.HasPrefix(digestHash, tagHash)
}

var oldDigestRegexp = regexp.MustCompile(`^Successfully built ([0-9a-f]+)\s*$`)

type dockerOutput struct {
	aux         *json.RawMessage
	shortDigest string
}

func indent(text, indent string) string {
	if text == "" {
		return indent + text
	}
	if text[len(text)-1:] == "\n" {
		result := ""
		for _, j := range strings.Split(text[:len(text)-1], "\n") {
			result += indent + j + "\n"
		}
		return result
	}
	result := ""
	for _, j := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		result += indent + j + "\n"
	}
	return result[:len(result)-1]
}

const DockerfileName = "Dockerfile"

// Creates a specification for the buildkit filesyncer
func toSyncedDirs(context string, dockerfileDir string, filter model.PathMatcher) []filesync.SyncedDir {
	fileMap := func(path string, s *fsutiltypes.Stat) bool {
		if !filepath.IsAbs(path) {
			path = filepath.Join(context, path)
		}

		matches, _ := filter.Matches(path)
		if matches {
			isDir := s != nil && s.IsDir()
			if !isDir {
				return false
			}

			entireDir, _ := filter.MatchesEntireDir(path)
			if entireDir {
				return false
			}
		}
		s.Uid = 0
		s.Gid = 0
		return true
	}

	return []filesync.SyncedDir{
		{
			Name: "context",
			Dir:  context,
			Map:  fileMap,
		},
		{
			Name: "dockerfile",
			Dir:  dockerfileDir,
		},
	}
}

// Writes a docker file to a temporary directory.
func writeTempDockerfile(contents string) (string, error) {
	// err is a named return value, due to the defer call below.
	dockerfileDir, err := ioutil.TempDir("", "tilt-tempdockerfile-")
	if err != nil {
		return "", fmt.Errorf("creating temp dockerfile directory: %v", err)
	}

	err = ioutil.WriteFile(filepath.Join(dockerfileDir, "Dockerfile"), []byte(contents), 0777)
	if err != nil {
		_ = os.RemoveAll(dockerfileDir)
		return "", fmt.Errorf("creating temp dockerfile: %v", err)
	}
	return dockerfileDir, nil
}
