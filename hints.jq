# Show only annotation hints that deviate from the spec's conservative defaults.
# Tools with no annotations are flagged, since clients must assume the worst case.
{readOnlyHint: false, destructiveHint: true, idempotentHint: false, openWorldHint: true} as $def
| .tools[]
| .name + "\t" + (
    if .annotations == null or .annotations == {} then
      "⚠ none declared → assume destructive, non-idempotent, open-world"
    else
      [ .annotations | to_entries[]
        | select($def[.key] != null)          # ignore title etc.
        | select(.value != $def[.key])        # drop values that restate a default
        | "\(.key)=\(.value)" ]
      | if length == 0 then "declared, but all values equal defaults"
        else join("  ") end
    end)
