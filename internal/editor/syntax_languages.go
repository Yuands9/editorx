package editor

import "strings"

type codeSyntaxOptions uint16

const (
	codeBlockComments codeSyntaxOptions = 1 << iota
	codeRawBackticks
	codeTemplateBackticks
	codeTripleQuotes
	codeTripleDoubleQuotes
	codeSQLQuotes
	codeCSSProperties
	codeFoldWords
	codeShellQuotes
	codePowerShellQuotes
	codePreprocessor
)

type codeWordKind uint8

const (
	codeKeyword codeWordKind = iota + 1
	codeType
	codeLiteral
)

type codeSyntax struct {
	name, extensions, lineComment string
	options                       codeSyntaxOptions
	words                         map[string]codeWordKind
}

const javascriptKeywords = "as async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield"
const javascriptTypes = "Array BigInt Boolean Date Error JSON Map Math Number Object Promise RegExp Set String Symbol WeakMap console"

var codeSyntaxes = [...]codeSyntax{
	{"Java", "java", "//", codeBlockComments | codeTripleDoubleQuotes,
		codeWords("abstract assert break case catch class const continue default do else enum exports extends final finally for goto if implements import instanceof interface module native new non-sealed open opens package permits private protected public record requires return sealed static strictfp super switch synchronized this throw throws to transient try uses var volatile while yield",
			"boolean byte char double float int long short void Boolean Byte Character Class Double Float Integer Long Object Short String System Throwable", "true false null")},
	{"Go", "go", "//", codeBlockComments | codeRawBackticks,
		codeWords("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
			"any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr", "true false nil iota")},
	{"SQL", "sql", "--", codeBlockComments | codeSQLQuotes | codeRawBackticks | codeFoldWords,
		codeWords("add all alter and as asc begin between by case check column commit create cross database declare default delete desc distinct drop else end except exists explain false fetch for foreign from full grant group having in index inner insert intersect into is join left like limit merge not null offset on or order outer over partition primary references returning revoke right rollback select set table then truncate union unique update using values view when where with",
			"bigint bit boolean char date datetime decimal double float int integer interval json jsonb numeric real smallint text time timestamp uuid varchar", "true false null")},
	{"JavaScript", "js mjs cjs jsx", "//", codeBlockComments | codeTemplateBackticks,
		codeWords(javascriptKeywords, javascriptTypes, "true false null undefined NaN Infinity")},
	{"TypeScript", "ts mts cts tsx", "//", codeBlockComments | codeTemplateBackticks,
		codeWords(javascriptKeywords+" abstract accessor asserts declare enum implements infer interface is keyof module namespace override private protected public readonly satisfies type",
			javascriptTypes+" any bigint boolean never number object string symbol unknown", "true false null undefined NaN Infinity")},
	{"CSS", "css", "", codeBlockComments | codeCSSProperties | codeFoldWords,
		codeWords("important media supports import layer namespace keyframes charset font-face", "", "inherit initial revert unset none auto transparent")},
	{"Python", "py pyw pyi", "#", codeTripleQuotes,
		codeWords("and as assert async await break case class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield",
			"bool bytes dict float frozenset int list object range set str tuple type", "True False None")},
	{"Shell", "sh bash zsh ksh", "#", codeShellQuotes | codeRawBackticks,
		codeWords("case do done elif else esac export fi for function if in local readonly return select then time typeset until unset while", "", "true false")},
	{"PowerShell", "ps1 psm1 psd1", "#", codePowerShellQuotes | codeFoldWords,
		codeWords("begin break catch class continue data do dynamicparam else elseif end enum exit filter finally for foreach from function if in param process return switch throw trap try using var while",
			"array bool byte char datetime decimal double float hashtable int long object string type", "$true $false $null")},
	{"C/C++", "c h cc cpp cxx hh hpp hxx", "//", codeBlockComments | codePreprocessor,
		codeWords("alignas alignof and asm auto break case catch class concept const constexpr continue default delete do else enum explicit export extern for friend goto if inline namespace new noexcept operator private protected public register requires return sizeof static struct switch template this throw try typedef typename union using virtual volatile while",
			"bool char char16_t char32_t double float int long short signed unsigned void wchar_t size_t", "true false NULL nullptr")},
	{"C#", "cs", "//", codeBlockComments | codeTripleDoubleQuotes | codePreprocessor,
		codeWords("abstract as async await base break case catch class const continue default delegate do else enum event explicit extern finally fixed for foreach from get global goto if implicit in interface internal is lock namespace new operator out override params partial private protected public readonly record ref required return sealed set sizeof stackalloc static struct switch this throw try using var virtual volatile where while yield",
			"bool byte char decimal double dynamic float int long object sbyte short string uint ulong ushort void", "true false null")},
	{"Kotlin", "kt kts", "//", codeBlockComments | codeTripleDoubleQuotes,
		codeWords("actual annotation as break by catch class companion const constructor continue data do else enum expect final finally for fun get if import in init interface internal is lateinit object open operator override package private protected public return sealed set super suspend this throw try typealias val var when where while",
			"Any Array Boolean Byte Char Double Float Int List Long Map Nothing Set Short String Unit", "true false null")},
	{"Rust", "rs", "//", codeBlockComments,
		codeWords("as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self static struct super trait type unsafe use where while",
			"bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize Box Option Result String Vec", "true false None Some Ok Err")},
}

var codeSyntaxByName, codeSyntaxByExtension = indexCodeSyntaxes()

func codeWords(keywords, types, literals string) map[string]codeWordKind {
	words := make(map[string]codeWordKind)
	for _, word := range strings.Fields(keywords) {
		words[word] = codeKeyword
	}
	for _, word := range strings.Fields(types) {
		words[word] = codeType
	}
	for _, word := range strings.Fields(literals) {
		words[word] = codeLiteral
	}
	return words
}

func indexCodeSyntaxes() (map[string]*codeSyntax, map[string]*codeSyntax) {
	names := make(map[string]*codeSyntax, len(codeSyntaxes))
	extensions := make(map[string]*codeSyntax)
	for index := range codeSyntaxes {
		syntax := &codeSyntaxes[index]
		names[syntax.name] = syntax
		for _, extension := range strings.Fields(syntax.extensions) {
			extensions["."+extension] = syntax
		}
	}
	return names, extensions
}
