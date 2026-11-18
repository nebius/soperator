#ifndef TEST_SPANK_H
#define TEST_SPANK_H

typedef void *spank_t;
typedef enum {
    ESPANK_SUCCESS,
    ESPANK_ERROR,
    ESPANK_ENV_NOEXIST
} spank_err_t;

spank_err_t spank_setenv(spank_t spank, const char *name, const char *value, int overwrite);
spank_err_t spank_unsetenv(spank_t spank, const char *name);

#endif
