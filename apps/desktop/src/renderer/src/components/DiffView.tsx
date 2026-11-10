function classify(line: string): string {
  if (line.startsWith('+++') || line.startsWith('---')) return 'meta'
  if (line.startsWith('@@')) return 'hunk'
  if (line.startsWith('+')) return 'add'
  if (line.startsWith('-')) return 'del'
  return ''
}

export function DiffView({ diff }: { diff: string }) {
  const lines = diff.split('\n')
  return (
    <div className="diff selectable">
      {lines.map((line, index) => (
        <div key={index} className={`diff-line ${classify(line)}`}>
          {line || ' '}
        </div>
      ))}
    </div>
  )
}
