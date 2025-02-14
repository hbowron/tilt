import { mount } from "enzyme"
import { MemoryRouter } from "react-router"
import {
  createFilterTermState,
  EMPTY_FILTER_TERM,
  FilterLevel,
  FilterSource,
} from "./logfilters"
import { LogUpdateAction } from "./LogStore"
import {
  OverviewLogComponent,
  PROLOGUE_LENGTH,
  renderWindow,
} from "./OverviewLogPane"
import {
  BuildLogAndRunLog,
  ManyLines,
  StyledLines,
  ThreeLines,
  ThreeLinesAllLog,
} from "./OverviewLogPane.stories"
import { newFakeRaf, RafProvider, SyncRafProvider } from "./raf"
import { appendLines } from "./testlogs"

let logPaneMount = (pane: any) => {
  return mount(
    <MemoryRouter initialEntries={["/"]}>
      <SyncRafProvider>{pane}</SyncRafProvider>
    </MemoryRouter>
  )
}

it("renders 3 lines in resource view", () => {
  let root = logPaneMount(<ThreeLines />)
  let el = root.getDOMNode()
  expect(el.querySelectorAll(".LogLine")).toHaveLength(3)
})

it("renders 3 lines in all log view", () => {
  let root = logPaneMount(<ThreeLinesAllLog />)
  let el = root.getDOMNode()
  expect(el.querySelectorAll(".LogLine")).toHaveLength(3)
})

it("escapes html and linkifies", () => {
  let root = logPaneMount(<StyledLines />)
  let el = root.getDOMNode()
  expect(el.querySelectorAll(".LogLine a")).toHaveLength(2)
  expect(el.querySelectorAll(".LogLine button")).toHaveLength(0)
})

it("filters by source", () => {
  let root = logPaneMount(<BuildLogAndRunLog />)
  let el = root.getDOMNode()
  expect(el.querySelectorAll(".LogLine")).toHaveLength(40)

  let root2 = logPaneMount(
    <BuildLogAndRunLog
      level=""
      source={FilterSource.runtime}
      term={EMPTY_FILTER_TERM}
    />
  )
  let el2 = root2.getDOMNode()
  expect(el2.querySelectorAll(".LogLine")).toHaveLength(20)
  expect(el2.innerHTML).toEqual(expect.stringContaining("Vigoda pod line"))
  expect(el2.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda build line")
  )

  let root3 = logPaneMount(
    <BuildLogAndRunLog
      level=""
      source={FilterSource.build}
      term={EMPTY_FILTER_TERM}
    />
  )
  let el3 = root3.getDOMNode()
  expect(el3.querySelectorAll(".LogLine")).toHaveLength(20)
  expect(el3.innerHTML).toEqual(expect.not.stringContaining("Vigoda pod line"))
  expect(el3.innerHTML).toEqual(expect.stringContaining("Vigoda build line"))
})

it("filters by level", () => {
  let root = logPaneMount(
    <BuildLogAndRunLog source="" level="" term={EMPTY_FILTER_TERM} />
  )
  let el = root.getDOMNode()
  expect(el.querySelectorAll(".LogLine")).toHaveLength(40)

  let root2 = logPaneMount(
    <BuildLogAndRunLog
      level={FilterLevel.warn}
      source=""
      term={EMPTY_FILTER_TERM}
    />
  )
  let el2 = root2.getDOMNode()
  expect(el2.querySelectorAll(".LogLine")).toHaveLength(
    2 * (1 + PROLOGUE_LENGTH)
  )

  let alertEnds = el2.querySelectorAll(".is-endOfAlert")
  let alertEnd = alertEnds[alertEnds.length - 1]
  expect(alertEnd.innerHTML).toEqual(
    expect.stringContaining("Vigoda pod warning line")
  )
  expect(alertEnd.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda pod line")
  )
  expect(alertEnd.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda pod error line")
  )

  let root3 = logPaneMount(
    <BuildLogAndRunLog
      level={FilterLevel.error}
      source=""
      term={EMPTY_FILTER_TERM}
    />
  )
  let el3 = root3.getDOMNode()
  expect(el3.querySelectorAll(".LogLine")).toHaveLength(
    2 * (1 + PROLOGUE_LENGTH)
  )

  alertEnds = el3.querySelectorAll(".is-endOfAlert")
  alertEnd = alertEnds[alertEnds.length - 1]
  expect(alertEnd.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda pod warning line")
  )
  expect(alertEnd.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda pod line")
  )
  expect(alertEnd.innerHTML).toEqual(
    expect.stringContaining("Vigoda pod error line")
  )
})

it("filters by term", () => {
  const noFilterRoot = logPaneMount(
    <BuildLogAndRunLog source="" level="" term={EMPTY_FILTER_TERM} />
  )
  const noTermEl = noFilterRoot.getDOMNode()
  expect(noTermEl.querySelectorAll(".LogLine")).toHaveLength(40)

  const termWithResults = createFilterTermState("line 5")
  const filterWithResults = logPaneMount(
    <BuildLogAndRunLog source="" level="" term={termWithResults} />
  )
  const elWithResults = filterWithResults.getDOMNode()

  expect(elWithResults.querySelectorAll(".LogLine")).toHaveLength(2)
  expect(elWithResults.innerHTML).toEqual(expect.stringContaining("line 5"))
  expect(elWithResults.innerHTML).toEqual(
    expect.not.stringContaining("line 15")
  )

  const termWithNoResults = createFilterTermState("spaghetti")
  const filterWithNoResults = logPaneMount(
    <BuildLogAndRunLog source="" level="" term={termWithNoResults} />
  )
  const elWithNoResults = filterWithNoResults.getDOMNode()

  expect(elWithNoResults.querySelectorAll(".LogLine")).toHaveLength(0)
  expect(elWithNoResults.innerHTML).toEqual(
    expect.not.stringContaining("Vigoda")
  )
})

it("engages autoscrolls on scroll down", () => {
  let fakeRaf = newFakeRaf()
  let root = mount(
    <MemoryRouter initialEntries={["/"]}>
      <RafProvider value={fakeRaf}>
        <ManyLines count={100} />
      </RafProvider>
    </MemoryRouter>
  )
  let component = root
    .find(OverviewLogComponent)
    .instance() as OverviewLogComponent

  component.autoscroll = false
  component.scrollTop = 0
  component.rootRef.current.scrollTop = 1000
  component.onScroll()
  expect(component.scrollTop).toEqual(1000)

  // The scroll has been scheduled, but not engaged yet.
  expect(component.autoscrollRafId).toBeGreaterThan(0)
  expect(component.autoscroll).toEqual(false)

  fakeRaf.invoke(component.autoscrollRafId as number)
  expect(component.autoscroll).toEqual(true)
})

it("renders bottom logs first", () => {
  let fakeRaf = newFakeRaf()
  let lineCount = 2 * renderWindow
  let root = mount(
    <MemoryRouter initialEntries={["/"]}>
      <RafProvider value={fakeRaf}>
        <ManyLines count={lineCount} />
      </RafProvider>
    </MemoryRouter>
  )

  // Make sure no logs have been rendered yet.
  let rootEl = root.getDOMNode()
  let lineEls = () => rootEl.querySelectorAll(".LogLine")
  let component = root
    .find(OverviewLogComponent)
    .instance() as OverviewLogComponent
  expect(component.renderBufferRafId).toBeGreaterThan(0)
  expect(component.backwardBuffer.length).toEqual(lineCount)
  expect(lineEls().length).toEqual(0)

  // Invoke the RAF callback, and make sure that only a window's
  // worth of logs have been rendered.
  fakeRaf.invoke(component.renderBufferRafId as number)
  expect(component.backwardBuffer.length).toEqual(lineCount - renderWindow)
  expect(lineEls().length).toEqual(renderWindow)
  expect(lineEls()[0].innerHTML).toEqual(
    expect.stringContaining(">line 250\n<")
  )

  // Invoke the RAF callback again, and make sure the remaining logs
  // were rendered.
  fakeRaf.invoke(component.renderBufferRafId as number)
  expect(component.backwardBuffer.length).toEqual(0)
  expect(lineEls().length).toEqual(lineCount)
  expect(lineEls()[0].innerHTML).toEqual(expect.stringContaining(">line 0\n<"))

  // rendering is complete.
  expect(component.renderBufferRafId).toEqual(0)
})

it("renders new logs first", () => {
  let fakeRaf = newFakeRaf()
  let initLineCount = 2 * renderWindow
  let root = mount(
    <MemoryRouter initialEntries={["/"]}>
      <RafProvider value={fakeRaf}>
        <ManyLines count={initLineCount} />
      </RafProvider>
    </MemoryRouter>
  )

  let rootEl = root.getDOMNode()
  let lineEls = () => rootEl.querySelectorAll(".LogLine")
  let component = root
    .find(OverviewLogComponent)
    .instance() as OverviewLogComponent
  expect(component.renderBufferRafId).toBeGreaterThan(0)
  expect(component.backwardBuffer.length).toEqual(initLineCount)
  expect(lineEls().length).toEqual(0)

  // append new lines on top of the lines we already have.
  let newLineCount = 1.5 * renderWindow
  let lines = []
  for (let i = 0; i < newLineCount; i++) {
    lines.push(`incremental line ${i}\n`)
  }
  appendLines(component.props.logStore, "fe", ...lines)
  component.onLogUpdate({ action: LogUpdateAction.append })
  expect(component.forwardBuffer.length).toEqual(newLineCount)
  expect(component.backwardBuffer.length).toEqual(initLineCount)

  // Invoke the RAF callback, and make sure that new logs were rendered
  // and old logs were rendered.
  fakeRaf.invoke(component.renderBufferRafId as number)
  expect(component.forwardBuffer.length).toEqual(newLineCount - renderWindow)
  expect(component.backwardBuffer.length).toEqual(initLineCount - renderWindow)
  expect(lineEls().length).toEqual(renderWindow * 2)
  expect(lineEls()[0].innerHTML).toEqual(
    expect.stringContaining(">line 250\n<")
  )
  expect(lineEls()[lineEls().length - 1].innerHTML).toEqual(
    expect.stringContaining(">incremental line 249\n<")
  )

  // Invoke the RAF callback again, and make sure that new logs were rendered further up
  // and old logs were rendered further down.
  fakeRaf.invoke(component.renderBufferRafId as number)
  expect(lineEls()[0].innerHTML).toEqual(expect.stringContaining(">line 0\n<"))
  expect(lineEls()[lineEls().length - 1].innerHTML).toEqual(
    expect.stringContaining(">incremental line 374\n<")
  )
})
