package k8smodels

import (
	"fmt"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type Image struct {
	Repository string
	Tag        string
}

func (i Image) String() string {
	return fmt.Sprintf("%s:%s", i.Repository, i.Tag)
}

func BuildImageFrom(spec *slurmv1.ImageSpec, componentType consts.ComponentType) (Image, error) {
	repository, tag, err := imageDefaultsFrom(componentType)
	if err != nil {
		return Image{}, err
	}

	if spec != nil {
		if spec.Repository != nil {
			repository = *spec.Repository
		}
		if spec.Tag != nil {
			tag = *spec.Tag
		}
	}

	return Image{
		Repository: repository,
		Tag:        tag,
	}, nil
}

func imageDefaultsFrom(componentType consts.ComponentType) (repository, tag string, err error) {
	switch componentType {
	case consts.ComponentTypeController:
		repository = consts.DefaultImageRepositoryController
		tag = consts.DefaultImageTagController
	case consts.ComponentTypeWorker:
		repository = consts.DefaultImageRepositoryWorker
		tag = consts.DefaultImageTagWorker
	default:
		err = fmt.Errorf("failed to get default image defaults for unknown component type %q", componentType)
	}

	return
}
