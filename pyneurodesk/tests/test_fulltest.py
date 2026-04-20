from __future__ import annotations

from pathlib import Path

from pyneurodesk.fulltest import (
    build_container_reference,
    cvmfs_path_from_source,
    default_recipe_path,
    image_cache_name,
    load_suite,
    substitute_variables,
    timeout_for,
)


def test_load_suite_parses_niimath_recipe() -> None:
    suite = load_suite(default_recipe_path())

    assert suite.name == "niimath"
    assert suite.container == "niimath_1.0.20250804_20251016.simg"
    assert len(suite.required_files) == 1
    assert len(suite.tests) > 50
    assert suite.tests[0].name == "Version check"
    assert suite.tests[0].expected_output_contains == ("niimath version",)


def test_build_container_reference_defaults_to_cvmfs_directory() -> None:
    suite = load_suite(default_recipe_path())

    reference = build_container_reference(
        suite,
        image_name="niimath",
        image_source="",
        mirror="https://cvmfs.neurodesk.org",
        repo="neurodesk.ardc.edu.au",
        cache_dir="/tmp/cvmfs-cache",
    )

    assert reference.image == "niimath"
    assert reference.source.type == "cvmfs"
    assert reference.source.path == "/containers/niimath_1.0.20250804_20251016"
    assert reference.cache_dir == "/tmp/cvmfs-cache"


def test_cvmfs_path_from_source_supports_cvmfs_uri() -> None:
    assert (
        cvmfs_path_from_source(
            "cvmfs://neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016"
        )
        == "/containers/niimath_1.0.20250804_20251016"
    )


def test_cvmfs_path_from_source_supports_http_mount_path() -> None:
    assert (
        cvmfs_path_from_source(
            "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016"
        )
        == "/containers/niimath_1.0.20250804_20251016"
    )


def test_substitute_variables_replaces_braced_and_unbraced_names() -> None:
    text = "cp ${input} $output"
    variables = {"input": "/work/in.nii.gz", "output": "/work/out.nii.gz"}

    assert substitute_variables(text, variables) == "cp /work/in.nii.gz /work/out.nii.gz"


def test_timeout_for_prefers_test_timeout_then_default() -> None:
    assert timeout_for(45, 90) == 45.0
    assert timeout_for(0, 90) == 90.0
    assert timeout_for(0, 0) == 120.0


def test_image_cache_name_is_stable() -> None:
    assert image_cache_name("niimath") == image_cache_name("niimath")
    assert image_cache_name("niimath") != image_cache_name("other")


def test_default_recipe_path_points_to_existing_recipe() -> None:
    recipe = default_recipe_path()

    assert recipe.is_file()
    assert recipe.name == "fulltest.yaml"
    assert recipe.parts[-5:] == ("local", "neurocontainers", "recipes", "niimath", "fulltest.yaml")
