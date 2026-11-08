#define PAGE_SIZE 4096 // +checkconst checkconst/layout PageSize
#define BAD_PAGE_SIZE 8192 // +checkconst checkconst/layout PageSize // want `got value 8192, wanted "4096"`
#define HEADER_ALIGN 4 // +checkalign checkconst/layout Header
#define BAD_HEADER_ALIGN 2 // +checkalign checkconst/layout Header // want `got value 2, wanted "4"`
#define FLAGS_OFFSET HEADER_BASE + 4 // +checkoffset checkconst/layout Header.Flags
#define BAD_KIND_OFFSET HEADER_BASE + 4 // +checkoffset checkconst/layout Header.Kind // want `got value 4, wanted "6"`
#define HEADER_SIZE 8 // +checksize checkconst/layout Header
#define HEX_HEADER_SIZE 0x8 // +checksize checkconst/layout Header // want `got value "0x8", wanted "8"`
#define MISSING 1 // +checkconst checkconst/layout Missing // want `fact of type const unavailable for "Missing"`
#define MALFORMED 1 // +checksize Header // want "potentially malformed checkconst directives"
