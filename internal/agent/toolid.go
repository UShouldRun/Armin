package agent

const (
  toolUnknown ToolID = iota
  toolWriteFile
  toolEditFile
  toolSearchCode
  toolReadFile
  toolListDirectory
  toolRunCommand
  toolWebSearch
  toolCountLines
  toolListLanguages
  toolFindFiles
  toolFileStat
  toolFindTodoFixme
  toolEnvInfo
  toolProjectManifest
  toolFindDefinition
  toolListSymbols
  toolOutlineFile
  toolFindReferences
  toolGitLog
  toolGitBlame
  toolGitDiff
  toolGitShow
  toolWorkingTreeDiff
  toolGrepHistory
  toolCodeOwners
  toolCommitStats
  toolRunTests
  toolBuildCheck
  toolLintCheck
  toolRunProgram
  toolListDependencies
  toolModuleGraph
  toolReadDocComment
  toolFindCallers
  toolFindDeadCode
  toolImpactAnalysis
  toolSemanticSearch
  toolReadPDF
  toolMemorySave
  toolMemoryRead
  toolMemoryDelete
  toolScratchpadWrite
  toolScratchpadRead
  toolScratchpadList
  toolUseSkill
  toolRequestAccess
  toolCount
)

// toolIDs maps wire names to IDs. A miss yields toolUnknown.
var toolIDs = map[string]ToolID{
  "write_file":      toolWriteFile,
  "edit_file":       toolEditFile,
  "search_code":     toolSearchCode,
  "read_file":       toolReadFile,
  "list_directory":  toolListDirectory,
  "run_command":     toolRunCommand,
  "web_search":      toolWebSearch,
  "count_lines":     toolCountLines,
  "list_languages":  toolListLanguages,
  "find_files":      toolFindFiles,
  "file_stat":       toolFileStat,
  "find_todo_fixme": toolFindTodoFixme,
  "env_info":        toolEnvInfo,
  "project_manifest": toolProjectManifest,
  "find_definition": toolFindDefinition,
  "list_symbols":    toolListSymbols,
  "outline_file":    toolOutlineFile,
  "find_references": toolFindReferences,
  "git_log":         toolGitLog,
  "git_blame":       toolGitBlame,
  "git_diff":        toolGitDiff,
  "git_show":        toolGitShow,
  "working_tree_diff": toolWorkingTreeDiff,
  "grep_history":    toolGrepHistory,
  "code_owners":     toolCodeOwners,
  "commit_stats":    toolCommitStats,
  "run_tests":       toolRunTests,
  "build_check":     toolBuildCheck,
  "lint_check":      toolLintCheck,
  "run_program":     toolRunProgram,
  "list_dependencies": toolListDependencies,
  "module_graph":    toolModuleGraph,
  "read_doc_comment": toolReadDocComment,
  "find_callers":    toolFindCallers,
  "find_dead_code":  toolFindDeadCode,
  "impact_analysis": toolImpactAnalysis,
  "read_pdf":        toolReadPDF,
  "semantic_search": toolSemanticSearch,
  "memory_save":     toolMemorySave,
  "memory_read":     toolMemoryRead,
  "memory_delete":   toolMemoryDelete,
  "scratchpad_write": toolScratchpadWrite,
  "scratchpad_read": toolScratchpadRead,
  "scratchpad_list": toolScratchpadList,
  "request_access":  toolRequestAccess,
  "use_skill":       toolUseSkill,
}

// isWrite reports whether id changes files (run_program builds and
// runs code, so it counts).
func (id ToolID) isWrite() bool {
  return id == toolWriteFile || id == toolEditFile || id == toolRunProgram
}
