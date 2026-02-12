/*
 * Basic integration test for libcc C bindings
 *
 * This test validates the core C API functionality:
 * - Library initialization/shutdown
 * - Hypervisor detection
 * - OCI client creation
 * - Image pulling
 * - Instance creation
 * - Command execution
 *
 * Compile: gcc -o test_basic test_basic.c -L./build -lcc -Wl,-rpath,./build
 * Run: ./test_basic
 */

#include "libcc.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>

#define TEST(name) printf("TEST: %s... ", name)
#define PASS() printf("PASSED\n")
#define FAIL(msg) do { printf("FAILED: %s\n", msg); exit(1); } while(0)
#define SKIP(msg) printf("SKIPPED: %s\n", msg)

/* Helper to check error */
static void check_error(cc_error_code code, cc_error* err, const char* context) {
    if (code != CC_OK) {
        fprintf(stderr, "%s failed: code=%d", context, code);
        if (err && err->message) {
            fprintf(stderr, ", message=%s", err->message);
        }
        if (err && err->op) {
            fprintf(stderr, ", op=%s", err->op);
        }
        if (err && err->path) {
            fprintf(stderr, ", path=%s", err->path);
        }
        fprintf(stderr, "\n");
        cc_error_free(err);
        exit(1);
    }
    cc_error_free(err);
}

int main(int argc, char** argv) {
    cc_error err = {0};
    cc_error_code code;

    printf("=== libcc C Bindings Test ===\n\n");

    /* Test 1: API version */
    TEST("cc_api_version");
    {
        const char* version = cc_api_version();
        if (version == NULL) FAIL("version is NULL");
        if (strcmp(version, "0.1.0") != 0) {
            char buf[128];
            snprintf(buf, sizeof(buf), "unexpected version: %s", version);
            FAIL(buf);
        }
        cc_free_string((char*)version);
        PASS();
    }

    /* Test 2: API version compatibility */
    TEST("cc_api_version_compatible");
    {
        if (!cc_api_version_compatible(0, 1)) FAIL("0.1 should be compatible");
        if (!cc_api_version_compatible(0, 0)) FAIL("0.0 should be compatible");
        if (cc_api_version_compatible(1, 0)) FAIL("1.0 should NOT be compatible");
        if (cc_api_version_compatible(0, 99)) FAIL("0.99 should NOT be compatible");
        PASS();
    }

    /* Test 3: Library initialization */
    TEST("cc_init");
    {
        code = cc_init();
        if (code != CC_OK) FAIL("init failed");
        PASS();
    }

    /* Test 4: Guest protocol version */
    TEST("cc_guest_protocol_version");
    {
        int ver = cc_guest_protocol_version();
        if (ver != 1) {
            char buf[64];
            snprintf(buf, sizeof(buf), "unexpected protocol version: %d", ver);
            FAIL(buf);
        }
        PASS();
    }

    /* Test 5: Hypervisor check */
    TEST("cc_supports_hypervisor");
    bool hypervisor_available = false;
    {
        code = cc_supports_hypervisor(&err);
        if (code == CC_OK) {
            hypervisor_available = true;
            PASS();
        } else if (code == CC_ERR_HYPERVISOR_UNAVAILABLE) {
            SKIP("hypervisor not available (expected in CI)");
            cc_error_free(&err);
        } else {
            check_error(code, &err, "cc_supports_hypervisor");
        }
    }

    /* Test 6: Query capabilities */
    TEST("cc_query_capabilities");
    {
        cc_capabilities caps = {0};
        code = cc_query_capabilities(&caps, &err);
        check_error(code, &err, "cc_query_capabilities");

        printf("(hypervisor=%s, arch=%s) ",
               caps.hypervisor_available ? "yes" : "no",
               caps.architecture ? caps.architecture : "unknown");

        if (caps.architecture) {
            cc_free_string((char*)caps.architecture);
        }
        PASS();
    }

    /* Test 7: Cancel token */
    TEST("cc_cancel_token");
    {
        cc_cancel_token token = cc_cancel_token_new();
        if (!CC_HANDLE_VALID(token)) FAIL("failed to create token");

        if (cc_cancel_token_is_cancelled(token)) FAIL("new token should not be cancelled");

        cc_cancel_token_cancel(token);
        if (!cc_cancel_token_is_cancelled(token)) FAIL("token should be cancelled after cancel()");

        cc_cancel_token_free(token);
        PASS();
    }

    /* Test 8: OCI client creation */
    TEST("cc_oci_client_new");
    cc_oci_client client = CC_HANDLE_INVALID(cc_oci_client);
    {
        code = cc_oci_client_new(&client, &err);
        check_error(code, &err, "cc_oci_client_new");
        if (!CC_HANDLE_VALID(client)) FAIL("client handle is invalid");
        PASS();
    }

    /* Test 9: OCI client cache dir */
    TEST("cc_oci_client_cache_dir");
    {
        char* cache_dir = cc_oci_client_cache_dir(client);
        if (cache_dir == NULL) FAIL("cache_dir is NULL");
        printf("(cache=%s) ", cache_dir);
        cc_free_string(cache_dir);
        PASS();
    }

    /* If no hypervisor, skip remaining tests */
    if (!hypervisor_available) {
        printf("\n=== Skipping VM tests (no hypervisor) ===\n");
        cc_oci_client_free(client);
        cc_shutdown();
        printf("\n=== All available tests passed! ===\n");
        return 0;
    }

    /* Test 10: Pull image */
    TEST("cc_oci_client_pull");
    cc_instance_source source = CC_HANDLE_INVALID(cc_instance_source);
    {
        code = cc_oci_client_pull(
            client,
            "alpine:latest",
            NULL,  /* opts */
            NULL,  /* progress_cb */
            NULL,  /* progress_user_data */
            CC_HANDLE_INVALID(cc_cancel_token),
            &source,
            &err
        );
        check_error(code, &err, "cc_oci_client_pull");
        if (!CC_HANDLE_VALID(source)) FAIL("source handle is invalid");
        PASS();
    }

    /* Test 11: Get image config */
    TEST("cc_source_get_config");
    {
        cc_image_config* config = NULL;
        code = cc_source_get_config(source, &config, &err);
        check_error(code, &err, "cc_source_get_config");

        if (config == NULL) FAIL("config is NULL");
        printf("(arch=%s) ", config->architecture ? config->architecture : "unknown");

        cc_image_config_free(config);
        PASS();
    }

    /* Test 12: Create instance */
    TEST("cc_instance_new");
    cc_instance inst = CC_HANDLE_INVALID(cc_instance);
    {
        cc_instance_options opts = {0};
        opts.memory_mb = 256;
        opts.cpus = 1;

        code = cc_instance_new(source, &opts, &inst, &err);
        if (code == CC_ERR_HYPERVISOR_UNAVAILABLE) {
            /* Hypervisor access denied (e.g., missing entitlements) */
            SKIP("hypervisor access denied");
            cc_error_free(&err);
            cc_instance_source_free(source);
            cc_oci_client_free(client);
            cc_shutdown();
            printf("\n=== All available tests passed! ===\n");
            return 0;
        }
        check_error(code, &err, "cc_instance_new");
        if (!CC_HANDLE_VALID(inst)) FAIL("instance handle is invalid");
        PASS();
    }

    /* Test 13: Instance ID */
    TEST("cc_instance_id");
    {
        char* id = cc_instance_id(inst);
        if (id == NULL) FAIL("id is NULL");
        printf("(id=%s) ", id);
        cc_free_string(id);
        PASS();
    }

    /* Test 14: Instance is running */
    TEST("cc_instance_is_running");
    {
        if (!cc_instance_is_running(inst)) FAIL("instance should be running");
        PASS();
    }

    /* Test 15: Create command */
    TEST("cc_cmd_new + cc_cmd_output");
    {
        const char* args[] = {"Hello from C bindings!", NULL};
        cc_cmd cmd = CC_HANDLE_INVALID(cc_cmd);

        code = cc_cmd_new(inst, "echo", args, &cmd, &err);
        check_error(code, &err, "cc_cmd_new");
        if (!CC_HANDLE_VALID(cmd)) FAIL("cmd handle is invalid");

        uint8_t* output = NULL;
        size_t len = 0;
        int exit_code = -1;

        code = cc_cmd_output(cmd, &output, &len, &exit_code, &err);
        check_error(code, &err, "cc_cmd_output");

        if (exit_code != 0) {
            char buf[64];
            snprintf(buf, sizeof(buf), "exit code %d", exit_code);
            FAIL(buf);
        }

        if (output == NULL || len == 0) FAIL("output is empty");

        /* Check output contains expected text */
        const char* expected = "Hello from C bindings!";
        if (strstr((char*)output, expected) == NULL) {
            printf("output: '%.*s'\n", (int)len, output);
            FAIL("output doesn't contain expected text");
        }

        cc_free_bytes(output);
        cc_cmd_free(cmd);
        PASS();
    }

    /* Test 16: Filesystem operations */
    TEST("cc_fs_write_file + cc_fs_read_file");
    {
        const char* test_path = "/root/test_file.txt";
        const char* test_data = "Hello, filesystem!";
        size_t test_len = strlen(test_data);

        /* Write file */
        code = cc_fs_write_file(inst, test_path, (const uint8_t*)test_data, test_len, 0644, &err);
        check_error(code, &err, "cc_fs_write_file");

        /* Read file back */
        uint8_t* read_data = NULL;
        size_t read_len = 0;
        code = cc_fs_read_file(inst, test_path, &read_data, &read_len, &err);
        check_error(code, &err, "cc_fs_read_file");

        if (read_len != test_len) {
            char buf[128];
            snprintf(buf, sizeof(buf), "length mismatch: expected %zu, got %zu", test_len, read_len);
            FAIL(buf);
        }

        if (memcmp(read_data, test_data, test_len) != 0) {
            FAIL("data mismatch");
        }

        cc_free_bytes(read_data);
        PASS();
    }

    /* Test 17: File stat */
    TEST("cc_fs_stat");
    {
        cc_file_info info = {0};
        code = cc_fs_stat(inst, "/root/test_file.txt", &info, &err);
        check_error(code, &err, "cc_fs_stat");

        if (info.size != 18) {  /* "Hello, filesystem!" is 18 bytes */
            char buf[64];
            snprintf(buf, sizeof(buf), "unexpected size: %ld", (long)info.size);
            FAIL(buf);
        }

        if (info.is_dir) FAIL("should not be a directory");

        cc_file_info_free(&info);
        PASS();
    }

    /* Test 18: Read directory */
    TEST("cc_fs_read_dir");
    {
        cc_dir_entry* entries = NULL;
        size_t count = 0;

        code = cc_fs_read_dir(inst, "/root", &entries, &count, &err);
        check_error(code, &err, "cc_fs_read_dir");

        printf("(%zu entries) ", count);

        if (count > 0 && entries == NULL) FAIL("entries is NULL but count > 0");

        cc_dir_entries_free(entries, count);
        PASS();
    }

    /* Test 19: Remove file */
    TEST("cc_fs_remove");
    {
        code = cc_fs_remove(inst, "/root/test_file.txt", &err);
        check_error(code, &err, "cc_fs_remove");
        PASS();
    }

    /* Test 20: Stdout pipe */
    TEST("cc_cmd_stdout_pipe");
    {
        const char* args[] = {"Hello from pipe!", NULL};
        cc_cmd cmd = CC_HANDLE_INVALID(cc_cmd);

        code = cc_cmd_new(inst, "echo", args, &cmd, &err);
        check_error(code, &err, "cc_cmd_new");

        cc_conn pipe = CC_HANDLE_INVALID(cc_conn);
        code = cc_cmd_stdout_pipe(cmd, &pipe, &err);
        check_error(code, &err, "cc_cmd_stdout_pipe");
        if (!CC_HANDLE_VALID(pipe)) FAIL("pipe handle is invalid");

        code = cc_cmd_start(cmd, &err);
        check_error(code, &err, "cc_cmd_start");

        /* Read from pipe */
        uint8_t buf[256];
        size_t n = 0;
        code = cc_conn_read(pipe, buf, sizeof(buf) - 1, &n, &err);
        check_error(code, &err, "cc_conn_read");

        buf[n] = '\0';
        if (strstr((char*)buf, "Hello from pipe!") == NULL) {
            printf("pipe output: '%s'\n", buf);
            FAIL("pipe output doesn't contain expected text");
        }

        cc_conn_close(pipe, &err);

        int exit_code = -1;
        code = cc_cmd_wait(cmd, &exit_code, &err);
        check_error(code, &err, "cc_cmd_wait");
        if (exit_code != 0) FAIL("exit code should be 0");

        PASS();
    }

    /* Test 21: Stdin pipe */
    TEST("cc_cmd_stdin_pipe + cc_cmd_stdout_pipe");
    {
        const char* args[] = {NULL};
        cc_cmd cmd = CC_HANDLE_INVALID(cc_cmd);

        code = cc_cmd_new(inst, "cat", args, &cmd, &err);
        check_error(code, &err, "cc_cmd_new");

        cc_conn stdin_pipe = CC_HANDLE_INVALID(cc_conn);
        code = cc_cmd_stdin_pipe(cmd, &stdin_pipe, &err);
        check_error(code, &err, "cc_cmd_stdin_pipe");

        cc_conn stdout_pipe = CC_HANDLE_INVALID(cc_conn);
        code = cc_cmd_stdout_pipe(cmd, &stdout_pipe, &err);
        check_error(code, &err, "cc_cmd_stdout_pipe");

        code = cc_cmd_start(cmd, &err);
        check_error(code, &err, "cc_cmd_start");

        /* Write to stdin pipe */
        const char* input = "echo test";
        size_t written = 0;
        code = cc_conn_write(stdin_pipe, (const uint8_t*)input, strlen(input), &written, &err);
        check_error(code, &err, "cc_conn_write");
        cc_conn_close(stdin_pipe, &err);

        /* Read from stdout pipe */
        uint8_t buf[256];
        size_t n = 0;
        code = cc_conn_read(stdout_pipe, buf, sizeof(buf) - 1, &n, &err);
        check_error(code, &err, "cc_conn_read");

        buf[n] = '\0';
        if (strstr((char*)buf, "echo test") == NULL) {
            printf("pipe output: '%s'\n", buf);
            FAIL("echo-back output doesn't match");
        }

        cc_conn_close(stdout_pipe, &err);

        int exit_code = -1;
        code = cc_cmd_wait(cmd, &exit_code, &err);
        check_error(code, &err, "cc_cmd_wait");
        if (exit_code != 0) FAIL("exit code should be 0");

        PASS();
    }

    /* Test 22: Close instance */
    TEST("cc_instance_close");
    {
        code = cc_instance_close(inst, &err);
        check_error(code, &err, "cc_instance_close");
        PASS();
    }

    /* Cleanup */
    cc_instance_source_free(source);
    cc_oci_client_free(client);
    cc_shutdown();

    printf("\n=== All tests passed! ===\n");
    return 0;
}
