#include "snccld_util_string.h"

#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

size_t snccld_remove_string_duplicates(char **arr, const size_t n) {
    if (n <= 1) {
        return n;
    }

    int write_idx = 0;

    // Move unique elements to the front.
    for (int i = 0; i < n; ++i) {
        bool is_dup = false;

        for (int j = 0; j < write_idx; ++j) {
            if (strcmp(arr[i], arr[j]) == 0) {
                is_dup = true;
                break;
            }
        }

        if (!is_dup) {
            arr[write_idx++] = arr[i];
        }
    }

    // Remove duplicate elements.
    for (int i = write_idx; i < n; ++i) {
        free(arr[i]);
    }

    return write_idx;
}
