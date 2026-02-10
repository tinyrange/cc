//! Build script for cc-vm crate.
//!
//! Builds and statically links against libcc from Go source.
//! Also builds cc-helper and places it next to the output binary.

use std::env;
use std::path::PathBuf;
use std::process::Command;

fn main() {
    let out_dir = PathBuf::from(env::var("OUT_DIR").unwrap());
    let manifest_dir = PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap());
    let project_root = manifest_dir.join("../..").canonicalize()
        .expect("Failed to find project root");

    // Build the static library
    let lib_path = build_libcc(&out_dir, &project_root);
    let lib_dir = lib_path.parent().unwrap();

    // Tell cargo to link against libcc (static)
    println!("cargo:rustc-link-search=native={}", lib_dir.display());
    println!("cargo:rustc-link-lib=static=cc");

    // On macOS, we need to link against several system frameworks
    #[cfg(target_os = "macos")]
    {
        println!("cargo:rustc-link-lib=framework=CoreFoundation");
        println!("cargo:rustc-link-lib=framework=Security");
        println!("cargo:rustc-link-lib=resolv");
    }

    // On Linux, link against pthread and other required libs
    #[cfg(target_os = "linux")]
    {
        println!("cargo:rustc-link-lib=pthread");
        println!("cargo:rustc-link-lib=dl");
        println!("cargo:rustc-link-lib=resolv");
    }

    // On Windows, link against libs required by Go runtime and net packages
    #[cfg(target_os = "windows")]
    {
        println!("cargo:rustc-link-lib=ws2_32");
        println!("cargo:rustc-link-lib=advapi32");
        println!("cargo:rustc-link-lib=ntdll");
        println!("cargo:rustc-link-lib=userenv");
    }

    // Build cc-helper and copy to target directory
    build_and_install_helper(&out_dir, &project_root);

    // Rerun if Go source files change
    let bindings_c_dir = manifest_dir.join("../c");
    if bindings_c_dir.exists() {
        println!("cargo:rerun-if-changed={}", bindings_c_dir.join("libcc.go").display());
        println!("cargo:rerun-if-changed={}", bindings_c_dir.join("instance_proxy.go").display());
        println!("cargo:rerun-if-changed={}", bindings_c_dir.join("error.go").display());
        println!("cargo:rerun-if-changed={}", bindings_c_dir.join("handles.go").display());
        println!("cargo:rerun-if-changed={}", bindings_c_dir.join("ipc/client.go").display());
    }

    // Rerun if helper source changes
    let helper_dir = project_root.join("cmd/cc-helper");
    if helper_dir.exists() {
        println!("cargo:rerun-if-changed={}", helper_dir.display());
    }
}

/// Build libcc as a static library from Go source.
fn build_libcc(out_dir: &PathBuf, project_root: &PathBuf) -> PathBuf {
    // Verify we're in the right place by checking for bindings/c
    let bindings_c = project_root.join("bindings/c");
    if !bindings_c.exists() {
        panic!(
            "Could not find Go source at {}. \
             Make sure you're building from within the cc repository.",
            bindings_c.display()
        );
    }

    let lib_path = if cfg!(target_os = "windows") {
        out_dir.join("cc.lib")
    } else {
        out_dir.join("libcc.a")
    };

    // Check if we need to rebuild
    if !needs_rebuild(&lib_path, &bindings_c) {
        return lib_path;
    }

    println!("cargo:warning=Building libcc.a from Go source...");

    // Build the static library using go build -buildmode=c-archive
    let status = Command::new("go")
        .args([
            "build",
            "-buildmode=c-archive",
            "-o",
            lib_path.to_str().unwrap(),
            "./bindings/c",
        ])
        .current_dir(project_root)
        .env("CGO_ENABLED", "1")
        .status()
        .expect("Failed to execute go build. Is Go installed?");

    if !status.success() {
        panic!(
            "Failed to build libcc. go build exited with status: {}",
            status
        );
    }

    println!("cargo:warning=Successfully built {}", lib_path.display());

    lib_path
}

/// Build cc-helper and install it to the target directory.
fn build_and_install_helper(out_dir: &PathBuf, project_root: &PathBuf) {
    let helper_name = if cfg!(target_os = "windows") {
        "cc-helper.exe"
    } else {
        "cc-helper"
    };

    // Build helper to OUT_DIR first
    let helper_build_path = out_dir.join(helper_name);

    // Check if helper source exists
    let helper_src = project_root.join("cmd/cc-helper");
    if !helper_src.exists() {
        println!("cargo:warning=cc-helper source not found, skipping helper build");
        return;
    }

    // Check if we need to rebuild
    let needs_build = !helper_build_path.exists() || {
        let helper_mtime = std::fs::metadata(&helper_build_path)
            .and_then(|m| m.modified())
            .ok();
        helper_mtime.map_or(true, |build_time| {
            // Check if any Go file in cmd/cc-helper is newer
            walkdir_newer_than(&helper_src, build_time)
        })
    };

    if needs_build {
        println!("cargo:warning=Building cc-helper...");

        let status = Command::new("go")
            .args([
                "build",
                "-o",
                helper_build_path.to_str().unwrap(),
                "./cmd/cc-helper",
            ])
            .current_dir(project_root)
            .status();

        match status {
            Ok(s) if s.success() => {
                println!("cargo:warning=Successfully built cc-helper");

                // On macOS, codesign with entitlements
                #[cfg(target_os = "macos")]
                {
                    let entitlements = project_root.join("tools/entitlements.xml");
                    if entitlements.exists() {
                        let sign_status = Command::new("codesign")
                            .args([
                                "--sign", "-",
                                "--entitlements", entitlements.to_str().unwrap(),
                                "--force",
                                helper_build_path.to_str().unwrap(),
                            ])
                            .status();

                        match sign_status {
                            Ok(s) if s.success() => {
                                println!("cargo:warning=Codesigned cc-helper with entitlements");
                            }
                            _ => {
                                println!("cargo:warning=Failed to codesign cc-helper");
                            }
                        }
                    }
                }
            }
            _ => {
                println!("cargo:warning=Failed to build cc-helper");
                return;
            }
        }
    }

    // Copy helper to target directory
    // OUT_DIR is something like target/debug/build/cc-vm-xxx/out
    // We want to copy to target/debug/ (and target/debug/examples/)
    if let Some(target_dir) = find_target_dir(out_dir) {
        let profile = env::var("PROFILE").unwrap_or_else(|_| "debug".to_string());
        let target_profile_dir = target_dir.join(&profile);

        // Copy to main target dir
        if target_profile_dir.exists() {
            let dest = target_profile_dir.join(helper_name);
            if copy_if_different(&helper_build_path, &dest) {
                println!("cargo:warning=Installed cc-helper to {}", dest.display());
            }

            // Also copy to examples dir if it exists
            let examples_dir = target_profile_dir.join("examples");
            if examples_dir.exists() {
                let dest = examples_dir.join(helper_name);
                if copy_if_different(&helper_build_path, &dest) {
                    println!("cargo:warning=Installed cc-helper to {}", dest.display());
                }
            }
        }
    }
}

/// Check if libcc needs to be rebuilt.
fn needs_rebuild(lib_path: &PathBuf, bindings_c: &PathBuf) -> bool {
    if !lib_path.exists() {
        return true;
    }

    let lib_mtime = match std::fs::metadata(lib_path).and_then(|m| m.modified()) {
        Ok(t) => t,
        Err(_) => return true,
    };

    let source_files = [
        bindings_c.join("libcc.go"),
        bindings_c.join("instance_proxy.go"),
        bindings_c.join("error.go"),
        bindings_c.join("handles.go"),
    ];

    source_files.iter().any(|src| {
        std::fs::metadata(src)
            .and_then(|m| m.modified())
            .map_or(true, |src_time| src_time > lib_mtime)
    })
}

/// Check if any file in a directory tree is newer than the given time.
fn walkdir_newer_than(dir: &PathBuf, threshold: std::time::SystemTime) -> bool {
    if let Ok(entries) = std::fs::read_dir(dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.is_dir() {
                if walkdir_newer_than(&path, threshold) {
                    return true;
                }
            } else if path.extension().map_or(false, |e| e == "go") {
                if let Ok(meta) = std::fs::metadata(&path) {
                    if let Ok(mtime) = meta.modified() {
                        if mtime > threshold {
                            return true;
                        }
                    }
                }
            }
        }
    }
    false
}

/// Find the target directory from OUT_DIR.
fn find_target_dir(out_dir: &PathBuf) -> Option<PathBuf> {
    // OUT_DIR is like: target/debug/build/cc-vm-xxx/out
    // We want: target/
    let mut dir = out_dir.clone();
    while let Some(parent) = dir.parent() {
        if parent.file_name().map_or(false, |n| n == "target") {
            return Some(parent.to_path_buf());
        }
        dir = parent.to_path_buf();
    }
    None
}

/// Copy a file if the contents are different or dest doesn't exist.
fn copy_if_different(src: &PathBuf, dest: &PathBuf) -> bool {
    // Check if dest exists and has same size
    if let (Ok(src_meta), Ok(dest_meta)) = (std::fs::metadata(src), std::fs::metadata(dest)) {
        if src_meta.len() == dest_meta.len() {
            // Same size, assume same content
            return false;
        }
    }

    // Copy the file
    if let Err(e) = std::fs::copy(src, dest) {
        println!("cargo:warning=Failed to copy cc-helper: {}", e);
        return false;
    }

    // Preserve executable permission
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if let Ok(meta) = std::fs::metadata(dest) {
            let mut perms = meta.permissions();
            perms.set_mode(0o755);
            let _ = std::fs::set_permissions(dest, perms);
        }
    }

    true
}
