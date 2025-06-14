/**
 * String manipulation utilities.
 */

#ifndef SNCCLD_UTIL_STRING_H
#define SNCCLD_UTIL_STRING_H

#include <stdio.h>

/**
 * @brief Remove duplicate strings from a string array in-place.
 *
 * Behaviour:
 *   - Keeps the first occurrence of each distinct string.
 *   - Order among unique strings is preserved.
 *
 * @param arr String array to remove duplicates.
 * @param n Number of elements in the array.
 *
 * @return New length of the array after duplicates removal.
 */
size_t snccld_remove_string_duplicates(char **arr, size_t n);

#endif // SNCCLD_UTIL_STRING_H
