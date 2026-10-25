#!/usr/bin/env node
// SHEYTAN-LA — scripts/qt-configure-probe.mjs — R2 (run #51, Task ID 45)
//
// Executes the ACTUAL upstream Qt 6.8.2 qt_add_* call graph under a Qt-free
// cmake configure, against the REAL ui/qt tree. The unmodified
// ui/qt/CMakeLists.txt is included via add_subdirectory(), so every
// qt_add_executable / qt_add_qml_module / qt_add_resources call runs with its
// exact arguments and order, through verbatim-extracted upstream definitions
// of the Qt 6.8.2 Core macros (qtbase@v6.8.2 src/corelib/Qt6CoreMacros.cmake)
// — including the deferred finalization chain that reaches
// Qt6CoreMacros.cmake:2212 (target_sources), the exact frame run #50 died in.
//
// The run-#50 lesson binds this probe: a subset model gave a false green.
// PROBE-OK here is necessary, never sufficient (I25/I13): local cmake has no
// Qt toolchain, so hosted configure/compile/acceptance stay authoritative.
// What IS real here: the upstream macro bodies, the real tree's call shape,
// the real file inventory. What is NOT real: the Qt packages themselves —
// every stand-in is listed in the probe report, never hidden.
//
// Contract: exit 0 + "PROBE-OK" in the summary on success; exit 1 +
// "PROBE-FAIL" plus the verbatim cmake output on any failure. The suite
// (scripts/installer-contract.test.mjs) asserts PROBE-CLEAN and that every
// qt_add_* call in ui/qt/CMakeLists.txt was probe-executed.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

const QT_TAG = 'v6.8.2';
const QT_MACRO_URL =
  `https://raw.githubusercontent.com/qt/qtbase/${QT_TAG}/src/corelib/Qt6CoreMacros.cmake`;

const EXTRACT_REAL = [
  // The resource path — the class that killed runs #49 (:317/:330 qrc-file
  // branch) and #50 (:2212 target_sources in the FILES branch).
  'qt6_add_resources',
  'qt_add_resources',
  '_qt_internal_process_resource',
  '_qt_internal_expose_source_file_to_ide',
  '_qt_internal_expose_deferred_files_to_ide',
  '__qt_get_relative_resource_path_for_file',
  '__qt_propagate_generated_resource',
  '__qt_internal_propagate_object_library',
  '__qt_internal_propagate_object_files',
  '__qt_internal_sanitize_resource_name',
  '__qt_internal_generate_init_resource_source_file',
  // The executable + finalization path — registers the deferred
  // qt6_finalize_target that flushes deferred files through :2212.
  'qt6_add_executable',
  'qt_add_executable',
  '_qt_internal_create_executable',
  '_qt_internal_disable_autorcc_zstd_when_not_supported',
  '_qt_internal_finalize_target_defer',
  'qt6_finalize_target',
  '_qt_internal_finalize_executable',
  '_qt_internal_finalize_source_groups',
];

// Upstream helpers referenced by the extracted bodies that live in OTHER
// upstream files (not Qt6CoreMacros.cmake). Stubs are explicit and reported —
// none of them is on the :2212 path.
const STUB_HELPERS = {
  __qt_internal_set_cmp0156:
    'policy helper from Qt6CoreConfigExtras; configure-time policy setup only',
  _qt_internal_warn_about_example_add_subdirectory:
    'example-tree warning from Qt6CoreConfigExtras; not defined in Qt6CoreMacros.cmake',
  _qt_internal_set_up_static_runtime_library:
    'MSVC static runtime linkage helper; build-time only',
  __qt_internal_collect_all_target_dependencies:
    'transitive dep walker for executable finalizers; probe Qt libs declare none',
  __qt_internal_apply_plugin_imports_finalizer_mode:
    'static plugin import finalizer mode; no Qt plugin machinery in probe',
  __qt_internal_process_dependency_object_libraries:
    'object-library dependency finalizer; no object libraries in probe',
};

function fail(msg) {
  console.error(`PROBE-FAIL: ${msg}`);
  process.exit(1);
}

function arg(flag, def) {
  const i = process.argv.indexOf(flag);
  return i >= 0 ? process.argv[i + 1] : def;
}

const repoRoot = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..');
const uiQtDir = path.resolve(arg('--tree', path.join(repoRoot, 'ui/qt')));
const cmakeBin = arg('--cmake', 'cmake');
const keep = process.argv.includes('--keep');
const buildDir = arg('--build-dir',
  fs.mkdtempSync(path.join(os.tmpdir(), 'sheytan-qt-configure-probe-')));

if (!fs.existsSync(path.join(uiQtDir, 'CMakeLists.txt'))) {
  fail(`ui/qt tree not found at ${uiQtDir}`);
}

// ---------------------------------------------------------------- upstream
const cacheDir = path.join(os.tmpdir(), 'sheytan-qt-probe-cache');
fs.mkdirSync(cacheDir, { recursive: true });
const macroFile = path.join(cacheDir, `Qt6CoreMacros-${QT_TAG}.cmake`);
if (!fs.existsSync(macroFile) || fs.statSync(macroFile).size < 10000) {
  process.stdout.write(`probe: fetching upstream ${QT_MACRO_URL}\n`);
  let res;
  try {
    res = await fetch(QT_MACRO_URL);
  } catch (e) {
    fail(`cannot fetch upstream macro source: ${e}`);
  }
  if (!res.ok) fail(`upstream macro fetch HTTP ${res.status} at ${QT_MACRO_URL}`);
  fs.writeFileSync(macroFile, Buffer.from(await res.arrayBuffer()));
}
const macroSource = fs.readFileSync(macroFile, 'utf8');

// ------------------------------------------------------------- extraction
function extractBlock(name) {
  const lines = macroSource.split('\n');
  const openRe = new RegExp(`^\\s*(function|macro)\\(${name}\\b`, 'i');
  for (let i = 0; i < lines.length; i++) {
    if (!openRe.test(lines[i])) continue;
    let depth = 0;
    const start = i;
    for (; i < lines.length; i++) {
      if (/^\s*(function|macro)\(/i.test(lines[i])) depth++;
      if (/^\s*end(function|macro)\(\)/i.test(lines[i])) {
        depth--;
        if (depth === 0) return lines.slice(start, i + 1).join('\n');
      }
    }
    fail(`unbalanced block for ${name}`);
  }
  return null;
}

const extracted = {};
for (const name of EXTRACT_REAL) {
  const block = extractBlock(name);
  if (!block) fail(`upstream definition of ${name} not found in ${QT_TAG} source`);
  extracted[name] = block;
}

// ----------------------------------------------------------- probe project
const probeSrc = path.join(buildDir, 'probe-src');
fs.mkdirSync(probeSrc, { recursive: true });

const stubs = `
# --- probe stand-ins (recorded; none of these is on the :2212 path) --------
set(QT_DEFAULT_MAJOR_VERSION 6)
set(QT_CMAKE_EXPORT_NAMESPACE "Qt6")
set(QT_FEATURE_zstd TRUE)
set_property(GLOBAL PROPERTY __qt_core_macros_module_base_dir
             "\${CMAKE_CURRENT_LIST_DIR}")

# Upstream template pair (shape-only): the resource path configure_file()s
# the .qrc content through Qt6CoreConfigureFileTemplate.in.
file(WRITE "\${CMAKE_CURRENT_LIST_DIR}/Qt6CoreConfigureFileTemplate.in"
     "@qt_core_configure_file_contents@")

macro(find_package QtPkg)
  set_property(GLOBAL APPEND PROPERTY SHEYTAN_PROBE_CALLS
               "find_package(\${QtPkg} \${ARGN})")
  if(NOT "\${QtPkg}" STREQUAL "Qt6")
    message(FATAL_ERROR "probe: unexpected find_package(\${QtPkg})")
  endif()
  cmake_parse_arguments(fp "" "" "COMPONENTS" \${ARGN})
  foreach(_c \${fp_COMPONENTS})
    if(NOT TARGET Qt6::\${_c})
      add_library(Qt6::\${_c} INTERFACE IMPORTED GLOBAL)
    endif()
  endforeach()
  set(Qt6_FOUND TRUE)
  set(Qt6_VERSION "6.8.2")
endmacro()

macro(qt_standard_project_setup)
  set_property(GLOBAL APPEND PROPERTY SHEYTAN_PROBE_CALLS
               "qt_standard_project_setup()")
  set(CMAKE_AUTOMOC ON)
endmacro()

macro(qt_add_qml_module target)
  set_property(GLOBAL APPEND PROPERTY SHEYTAN_PROBE_CALLS
               "qt_add_qml_module(\${target} \${ARGN})")
  cmake_parse_arguments(qm "NO_RESOURCE_TARGET_PATH" "URI;VERSION" "QML_FILES" \${ARGN})
  foreach(_f \${qm_QML_FILES})
    if(NOT EXISTS "\${CMAKE_CURRENT_SOURCE_DIR}/\${_f}")
      message(FATAL_ERROR "probe: qt_add_qml_module file missing at BASE: \${_f}")
    endif()
  endforeach()
  string(REPLACE "." "/" _qml_prefix "\${qm_URI}")
  # The REAL public entry — upstream qt6_add_resources — runs the QML module
  # files through the same _qt_internal_process_resource / deferred-IDE /
  # finalization machinery the style resources use.
  qt6_add_resources(\${target} probe_qml_module
      PREFIX "/qt/qml/\${_qml_prefix}"
      BASE "\${CMAKE_CURRENT_SOURCE_DIR}"
      FILES \${qm_QML_FILES})
endmacro()

macro(qt_generate_deploy_qml_app_script)
  set_property(GLOBAL APPEND PROPERTY SHEYTAN_PROBE_CALLS
               "qt_generate_deploy_qml_app_script(\${ARGV})")
endmacro()

add_executable(Qt6::rcc IMPORTED GLOBAL)
set_property(TARGET Qt6::rcc PROPERTY IMPORTED_LOCATION "/usr/bin/true")
`;

const helperStubs = Object.entries(STUB_HELPERS)
  .map(([n, why]) => `function(${n}) # stub: ${why}\nendfunction()`)
  .join('\n');

const assertions = `
# --- post-configure assertions ---------------------------------------------
get_property(_calls GLOBAL PROPERTY SHEYTAN_PROBE_CALLS)
list(LENGTH _calls _call_count)
if(_call_count EQUAL 0)
  message(FATAL_ERROR "probe: no qt_add_* calls were executed")
endif()
message(STATUS "probe: recorded calls: \${_calls}")

foreach(_t sheytan-ui sheytan-ui-acceptance)
  if(NOT TARGET "\${_t}")
    message(FATAL_ERROR "probe: target \${_t} was not created")
  endif()
endforeach()

# sheytan-ui is created by (real) qt_add_executable: the deferred
# qt6_finalize_target must have run through the upstream :2212 flush.
get_target_property(_final sheytan-ui _qt_is_finalized)
if(NOT _final)
  message(FATAL_ERROR "probe: sheytan-ui was not finalized — the deferred \
qt6_finalize_target chain did not run")
endif()

# I25 REAL-MACRO ROW: the argument set the upstream :2212 frame consumed.
# Upstream _qt_internal_expose_deferred_files_to_ide never clears the
# property, so assert instead that every deferred path RESOLVES — the exact
# class run #50 died of hosted (the tree's FILES form materialized
# non-existent ui/qt/<file>.qml paths into this list).
get_target_property(_deferred sheytan-ui _qt_deferred_files)
if(NOT _deferred)
  message(FATAL_ERROR "probe: sheytan-ui deferred list is empty — \
the resource path did not run")
endif()
foreach(_f IN LISTS _deferred)
  if(NOT EXISTS "\${_f}")
    message(FATAL_ERROR "probe: deferred file fed to the :2212 target_sources \
does not exist (run-#50 configure class): \${_f}")
  endif()
endforeach()
message(STATUS "probe: all \${_deferred} deferred files exist at their \
resolved paths")

get_target_property(_ui_srcs sheytan-ui SOURCES)
list(LENGTH _ui_srcs _ui_src_count)
if(_ui_src_count LESS 1)
  message(FATAL_ERROR "probe: sheytan-ui has no sources — resource attach failed")
endif()
message(STATUS "SHEYTAN-QT-CONFIGURE-PROBE: PROBE-OK")
`;

const extractedBlock = Object.values(extracted).join('\n\n');
fs.writeFileSync(
  path.join(probeSrc, 'qt6-core-macros-extracted.cmake'),
  `# VERBATIM extraction from qtbase ${QT_TAG} src/corelib/Qt6CoreMacros.cmake\n` +
  `# (fetched from ${QT_MACRO_URL}). Do not edit — regenerate via probe.\n\n` +
  extractedBlock + '\n');

const probeCMake = [
  'cmake_minimum_required(VERSION 3.21)',
  'project(sheytan-qt-configure-probe LANGUAGES CXX)',
  stubs,
  helperStubs,
  `include("\${CMAKE_CURRENT_LIST_DIR}/qt6-core-macros-extracted.cmake")`,
  `add_subdirectory("\${SHEYTAN_UI_QT_DIR}" sheytan-ui)`,
  assertions,
  '',
].join('\n\n');
fs.writeFileSync(path.join(probeSrc, 'CMakeLists.txt'), probeCMake);

// ----------------------------------------------------------------- run cmake
const cmakeArgs = [
  '-S', probeSrc,
  '-B', path.join(buildDir, 'build'),
  `-DSHEYTAN_UI_QT_DIR=${uiQtDir}`,
];
const run = spawnSync(cmakeBin, cmakeArgs, { encoding: 'utf8' });
const output = `${run.stdout || ''}${run.stderr || ''}`;
fs.writeFileSync(path.join(buildDir, 'probe-configure-output.txt'), output);

const ok = run.status === 0 && output.includes('SHEYTAN-QT-CONFIGURE-PROBE: PROBE-OK');

// ------------------------------------------------------------------- report
console.log('='.repeat(72));
console.log(`SHEYTAN Qt configure probe — upstream ${QT_TAG}, real tree ${uiQtDir}`);
console.log(`cmake: ${cmakeBin} ${cmakeArgs.join(' ')}`);
console.log(`cmake exit: ${run.status}`);
console.log(`verdict: ${ok ? 'PROBE-OK' : 'PROBE-FAIL'}`);
console.log('-'.repeat(72));
console.log('Real upstream definitions executed verbatim:');
for (const n of EXTRACT_REAL) console.log(`  [real]    ${n}`);
console.log('Explicit stand-ins (recorded, off the :2212 path):');
for (const [n, why] of Object.entries(STUB_HELPERS)) console.log(`  [stub]    ${n} — ${why}`);
console.log('  [stub]    find_package(Qt6 ...) — creates Qt6::<comp> INTERFACE targets');
console.log('  [stub]    qt_standard_project_setup — AUTOMOC mirror');
console.log('  [stub]    qt_add_qml_module — existence check + routes QML_FILES');
console.log('            through the REAL qt6_add_resources');
console.log('  [stub]    qt_generate_deploy_qml_app_script — recorded no-op (the tree');
console.log('            guards it behind QT_DEPLOY_AND_INSTALL; the probe leaves it unset)');
console.log('  [stub]    Qt6::rcc — /usr/bin/true IMPORTED executable');
console.log('-'.repeat(72));
if (!ok) {
  console.log('VERBATIM cmake output (tail):');
  console.log(output.split('\n').slice(-60).join('\n'));
}
console.log('='.repeat(72));

if (!keep && ok) {
  try { fs.rmSync(buildDir, { recursive: true, force: true }); } catch {}
}

process.exit(ok ? 0 : 1);
