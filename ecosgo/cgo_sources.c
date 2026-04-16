/*
 * cgo amalgamation: pulls the vendored ECOS C sources into this Go package's
 * cgo build so downstream consumers can `go get` without a separate `make`.
 *
 * All sources are compiled with -DDLONG -DLDL_LONG (set via cgo CFLAGS in
 * ecos.go), which selects the SuiteSparse_long/long-int variants of AMD and
 * LDL and is required for the ECOS types to match what cgo sees.
 */

/* SuiteSparse AMD: ordering. Compiled with -DDLONG so symbols are amd_l_*. */
#include "../ecos/external/amd/src/amd_global.c"
#include "../ecos/external/amd/src/amd_1.c"
#include "../ecos/external/amd/src/amd_2.c"
#include "../ecos/external/amd/src/amd_aat.c"
#include "../ecos/external/amd/src/amd_control.c"
#include "../ecos/external/amd/src/amd_defaults.c"
#include "../ecos/external/amd/src/amd_dump.c"
#include "../ecos/external/amd/src/amd_info.c"
#include "../ecos/external/amd/src/amd_order.c"
#include "../ecos/external/amd/src/amd_post_tree.c"
#include "../ecos/external/amd/src/amd_postorder.c"
#include "../ecos/external/amd/src/amd_preprocess.c"
#include "../ecos/external/amd/src/amd_valid.c"

/* Tim Davis' LDL: sparse LDL' factorization. -DLDL_LONG gives ldl_l_*. */
#include "../ecos/external/ldl/src/ldl.c"

/* ECOS core. */
#include "../ecos/src/cone.c"
#include "../ecos/src/ctrlc.c"
#include "../ecos/src/ecos.c"
#include "../ecos/src/equil.c"
#include "../ecos/src/expcone.c"
#include "../ecos/src/kkt.c"
#include "../ecos/src/preproc.c"
#include "../ecos/src/spla.c"
#include "../ecos/src/splamm.c"
#include "../ecos/src/timer.c"
#include "../ecos/src/wright_omega.c"

/* ECOS_BB: branch-and-bound MIP extension. */
#include "../ecos/ecos_bb/ecos_bb.c"
#include "../ecos/ecos_bb/ecos_bb_preproc.c"
