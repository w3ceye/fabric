/*
Copyright DTCC 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package java

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/chaincode/platforms/ccmetadata"
	"github.com/hyperledger/fabric/core/chaincode/platforms/util"
	cutil "github.com/hyperledger/fabric/core/container/util"
	pb "github.com/hyperledger/fabric/protos/peer"
)

var logger = flogging.MustGetLogger("chaincode.platform.java")

// Platform for java chaincodes in java
type Platform struct{}

// Name returns the name of this platform
func (javaPlatform *Platform) Name() string {
	return pb.ChaincodeSpec_JAVA.String()
}

//ValidatePath validates the java chaincode paths
func (javaPlatform *Platform) ValidatePath(rawPath string) error {
	path, err := url.Parse(rawPath)
	if err != nil || path == nil {
		logger.Errorf("invalid chaincode path %s %v", rawPath, err)
		return fmt.Errorf("invalid path: %s", err)
	}

	return nil
}

func (javaPlatform *Platform) ValidateCodePackage(code []byte) error {
	if len(code) == 0 {
		// Nothing to validate if no CodePackage was included
		return nil
	}

	// File to be valid should match first RegExp and not match second one.
	filesToMatch := regexp.MustCompile(`^(/)?src/((src|META-INF)/.*|(build\.gradle|settings\.gradle|pom\.xml))`)
	filesToIgnore := regexp.MustCompile(`.*\.class$`)
	is := bytes.NewReader(code)
	gr, err := gzip.NewReader(is)
	if err != nil {
		return fmt.Errorf("failure opening codepackage gzip stream: %s", err)
	}
	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// --------------------------------------------------------------------------------------
		// Check name for conforming path
		// --------------------------------------------------------------------------------------
		if !filesToMatch.MatchString(header.Name) || filesToIgnore.MatchString(header.Name) {
			return fmt.Errorf("illegal file detected in payload: \"%s\"", header.Name)
		}

		// --------------------------------------------------------------------------------------
		// Check that file mode makes sense
		// --------------------------------------------------------------------------------------
		// Acceptable flags:
		//      ISREG      == 0100000
		//      -rw-rw-rw- == 0666
		//
		// Anything else is suspect in this context and will be rejected
		// --------------------------------------------------------------------------------------
		if header.Mode&^0100666 != 0 {
			return fmt.Errorf("illegal file mode detected for file %s: %o", header.Name, header.Mode)
		}
	}
	return nil
}

// WritePackage writes the java chaincode package
func (javaPlatform *Platform) GetDeploymentPayload(path string) ([]byte, error) {
	logger.Debugf("Packaging java project from path %s", path)

	if path == "" {
		logger.Error("ChaincodeSpec's path cannot be empty")
		return nil, errors.New("ChaincodeSpec's path cannot be empty")
	}

	// trim trailing slash if it exists
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	excludedDirs := []string{"target", "build", "out"}
	excludedFileTypes := map[string]bool{".class": true}
	err := cutil.WriteFolderToTarPackage(tw, path, excludedDirs, nil, excludedFileTypes)
	if err != nil {
		logger.Errorf("Error writing java project to tar package %s", err)
		return nil, fmt.Errorf("failed to create chaincode package: %s", err)
	}

	tw.Close()
	gw.Close()

	return buf.Bytes(), nil
}

func (javaPlatform *Platform) GenerateDockerfile() (string, error) {
	var buf []string

	buf = append(buf, "FROM "+cutil.GetDockerfileFromConfig("chaincode.java.runtime"))
	buf = append(buf, "ADD binpackage.tar /root/chaincode-java/chaincode")

	dockerFileContents := strings.Join(buf, "\n")

	return dockerFileContents, nil
}

func (javaPlatform *Platform) GenerateDockerBuild(path string, code []byte, tw *tar.Writer) error {
	codepackage := bytes.NewReader(code)
	binpackage := bytes.NewBuffer(nil)
	buildOptions := util.DockerBuildOptions{
		Image:        cutil.GetDockerfileFromConfig("chaincode.java.runtime"),
		Cmd:          "./build.sh",
		InputStream:  codepackage,
		OutputStream: binpackage,
	}
	logger.Debugf("Executing docker build %v, %v", buildOptions.Image, buildOptions.Cmd)
	err := util.DockerBuild(buildOptions)
	if err != nil {
		logger.Errorf("Can't build java chaincode %v", err)
		return err
	}

	resultBytes := binpackage.Bytes()
	return cutil.WriteBytesToPackage("binpackage.tar", resultBytes, tw)
}

// GetMetadataProvider fetches metadata provider given deployment spec
func (javaPlatform *Platform) GetMetadataAsTarEntries(code []byte) ([]byte, error) {
	metadataProvider := &ccmetadata.TargzMetadataProvider{Code: code}
	return metadataProvider.GetMetadataAsTarEntries()
}
