// ToolID: a tool's index into the tool table (tools.go), a uint8 so
// the table is a fixed array, and the flags saying when each tool is
// offered.

package agent

// Tool IDs, in the order the model sees the tools (Definitions walks
// them in order, so moving one changes the prompt). Each has a spec
// in one of the tools_*.go files; tools.go indexes them.
const (
  toolUnknown ToolID = iota

  toolSearchCode
  toolReadFile
  toolReadPDF
  toolListDirectory
  toolRunCommand
  toolRunProgram
  toolEditFile
  toolWriteFile
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
  toolListDependencies
  toolModuleGraph
  toolReadDocComment
  toolFindCallers
  toolFindDeadCode
  toolImpactAnalysis
  toolSemanticSearch

  // offered only with the Loop's stores (flagSession)
  toolMemorySave
  toolMemoryRead
  toolMemoryDelete
  toolScratchpadWrite
  toolScratchpadRead
  toolScratchpadList
  toolRequestAccess
  toolUseSkill
  toolLibrarySearch
  toolLibraryTopics

  // offered only with the project's knowledge DAG (flagKnow)
  toolKnowFind
  toolKnowWrite
  toolKnowTrace

  // offered only with a minion to delegate to (flagMinion)
  toolDelegate

  // offered only with the learner tree (flagLearner)
  toolLearnExplained

  toolCount
)
