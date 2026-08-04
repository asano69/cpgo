# Overview

アーカイブ目的で使う安全なcpコマンド

* チェックサム検知で絶対にコピー中にファイルが壊れていることを検出する
* コピーが中断しても途中から再開可能で、冪等性がある。
* 全体進捗がわかる。
* 所有権、パーミション、リンクなど、できるだけ多くの属性をコピーする。変わってしまうときにWARNログを表示する。

## Rules

- 後方互換性は維持しなくてよい。
- When fixing bugs, add a failing regression test first.
- All errors are user-facing, so messages should be clear.
- Keep functions small and focused.
- Module files should re-export what's needed, hide implementation details.
- 変更内容を Codex形式(Search/Replace形式)で出力してください。
例）
```
mathweb/flask/app.py
<<<<<<< SEARCH
from flask import Flask
=======
import math
from flask import Flask
>>>>>>> REPLACE
```


