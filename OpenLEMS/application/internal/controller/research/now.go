package research

import "time"

// nowFunc 便于单测固定「今天」：滞后天数与「缺哪些日」都依赖它。
var nowFunc = time.Now
