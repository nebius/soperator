#include "snccld_util_host.h"

#include <stdlib.h>
#include <string.h>
#include <unistd.h>

char *snccld_get_hostname() {
#ifdef HOST_NAME_MAX
    size_t len = HOST_NAME_MAX + 1;
#else
    size_t len = 256;
#endif

    char *res = malloc(len);
    if (gethostname(res, len) != 0) {
        res = strdup("unknown");
    }
    return res;
}
