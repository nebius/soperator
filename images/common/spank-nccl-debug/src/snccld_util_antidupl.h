#ifndef SNCCLD_ANTIDUPL_H
#define SNCCLD_ANTIDUPL_H

#include <stdbool.h>
#include <string.h>

/**
 * snccld_remove_string_duplicates:
 *   Remove duplicate strings from an array in place.
 *
 * @param arr
 *   An array of pointers to NUL-terminated strings.
 * @param n
 *   Number of elements in arr.
 * @return
 *   The new length of the array after duplicates are removed.
 *
 * Behavior:
 *   - Keeps the first occurrence of each distinct string.
 *   - Order among unique strings is preserved.
 */
static size_t snccld_remove_string_duplicates(char **arr, size_t n) {
    if (n <= 1) {
        return n;
    }

    int write_idx = 0;

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

    return write_idx;
}

#endif // SNCCLD_ANTIDUPL_H
