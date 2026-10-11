// SPDX-License-Identifier: Apache-2.0
//
// Package humanize renders byte counts for UIs and logs. v1.1.4:
// consolidated — three identical private implementations lived in
// attachments, chunking and termshell, plus a variant in the tools
// archive package.
//
// v1.6.1 licensing: this package is an explicitly designated OPEN
// component under the repository's conservative mixed model — Apache-2.0
// (see LICENSE.md §2 "Open components"). It is pure, self-contained
// formatting logic with no coupling to any SHEYTAN subsystem.
//
// Copyright 2024-2026 Parsaetak
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package humanize

import "fmt"

// Bytes renders a human size: "4.4 GB", "12 MB", "3.5 KB", "780 B".
func Bytes(n int64) string {
        switch {
        case n >= 1<<30:
                return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
        case n >= 1<<20:
                return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
        case n >= 1<<10:
                return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
        default:
                return fmt.Sprintf("%d B", n)
        }
}
