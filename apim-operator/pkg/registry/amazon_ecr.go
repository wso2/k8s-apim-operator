package registry

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/go-logr/logr"
	"github.com/wso2/k8s-apim-operator/apim-operator/pkg/registry/utils"
	corev1 "k8s.io/api/core/v1"
	"strings"
)

const AmazonECR Type = "AMAZON_ECR"

var amazonEcr = &Config{
	RegistryType: AmazonECR,
	VolumeMounts: []corev1.VolumeMount{
		{
			Name:      "amazon-cred-helper",
			MountPath: "/kaniko/.docker/",
			ReadOnly:  true,
		},
		{
			Name:      "aws-credentials",
			MountPath: "/root/.aws/",
			ReadOnly:  true,
		},
	},
	Volumes: []corev1.Volume{
		{
			Name: "amazon-cred-helper",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: utils.ConfigJsonVolume,
					},
				},
			},
		},
		{
			Name: "aws-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: utils.AwsCredentialsVolume,
				},
			},
		},
	},
	IsImageExist: func(config *Config, auth utils.RegAuth, image string, tag string, logger logr.Logger) (bool, error) {
		repoNameSplits := strings.Split(repositoryName, ".")
		awsRegistryId := repoNameSplits[0]
		awsRegion := repoNameSplits[3]
		awsRepoName := strings.Split(repositoryName, "/")[1]

		sess, err := session.NewSession(&aws.Config{
			Region: aws.String(awsRegion)},
		)
		if err != nil {
			logger.Error(err, "Error creating aws session")
			return false, err
		}

		svc := ecr.New(sess)
		images, err := svc.ListImages(&ecr.ListImagesInput{
			RegistryId:     &awsRegistryId,
			RepositoryName: &awsRepoName,
		})
		if err != nil {
			logger.Error(err, "Error getting list of images in AWS ECR repository", "RegistryId", awsRegistryId, "RepositoryName", awsRepoName)
			return false, err
		}

		for _, id := range images.ImageIds {
			// found the image with tag
			logger.Info("Found the image tag from the AWS ECR repository", "RegistryId", awsRegistryId, "RepositoryName", awsRepoName, "image", imageName, "tag", tag)
			if *id.ImageTag == fmt.Sprintf("%s-%s", imageName, tag) {
				return true, nil
			}
		}

		// not found the image with tag
		return false, nil
	},
}

func amazonEcrFunc(repoName string, imgName string, tag string) *Config {
	// repository = <aws_account_id.dkr.ecr.region.amazonaws.com>/repository"
	// image path = <aws_account_id.dkr.ecr.region.amazonaws.com>/repository:imageName-v1"
	amazonEcr.ImagePath = fmt.Sprintf("%s:%s-%s", repoName, imgName, tag)
	return amazonEcr
}

func init() {
	addRegistryConfig(AmazonECR, amazonEcrFunc)
}