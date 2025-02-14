import React, { useCallback } from "react"
import styled from "styled-components"
import { Tags } from "./analytics"
import { ReactComponent as TriggerButtonManualSvg } from "./assets/svg/trigger-button-manual.svg"
import { ReactComponent as TriggerButtonSvg } from "./assets/svg/trigger-button.svg"
import { InstrumentedButton } from "./instrumentedComponents"
import {
  AnimDuration,
  Color,
  mixinResetButtonStyle,
  overviewItemBorderRadius,
  SizeUnit,
} from "./style-helpers"
import { triggerTooltip } from "./trigger"
import { TriggerMode } from "./types"

export let SidebarTriggerButtonRoot = styled(InstrumentedButton)`
  ${mixinResetButtonStyle};
  width: ${SizeUnit(1)};
  height: ${SizeUnit(1)};
  background-color: ${Color.grayLighter};
  border-bottom-left-radius: ${overviewItemBorderRadius};
  border-top-right-radius: ${overviewItemBorderRadius};
  display: flex;
  align-items: center;
  flex-shrink: 0;
  justify-content: center;
  opacity: 0;
  pointer-events: none;

  &.is-clickable {
    pointer-events: auto;
    cursor: pointer;
  }
  &.is-clickable,
  &.is-queued {
    opacity: 1;
  }
  &.is-selected {
    background-color: ${Color.gray7};
  }
  &:hover {
    background-color: ${Color.grayDark};
  }
  &.is-selected:hover {
    background-color: ${Color.grayLightest};
  }

  & .fillStd {
    transition: fill ${AnimDuration.default} ease;
    fill: ${Color.grayLight};
  }
  &.is-manual .fillStd {
    fill: ${Color.blue};
  }
  &.is-selected .fillStd {
    fill: ${Color.black};
  }
  &:hover .fillStd {
    fill: ${Color.white};
  }
  &.is-selected:hover .fillStd {
    fill: ${Color.blueDark};
  }
  & > svg {
    transition: transform ${AnimDuration.short} linear;
  }
  &:active > svg {
    transform: scale(1.2);
  }
  &.is-queued > svg {
    animation: spin 1s linear infinite;
  }
`

type SidebarTriggerButtonProps = {
  isTiltfile: boolean
  isBuilding: boolean
  hasBuilt: boolean
  triggerMode: TriggerMode
  isSelected: boolean
  hasPendingChanges: boolean
  isQueued: boolean
  onTrigger: () => void
  analyticsTags: Tags
}

function SidebarTriggerButton(props: SidebarTriggerButtonProps) {
  let isManual =
    props.triggerMode === TriggerMode.TriggerModeManual ||
    props.triggerMode === TriggerMode.TriggerModeManualWithAutoInit
  let isAutoInit =
    props.triggerMode === TriggerMode.TriggerModeAuto ||
    props.triggerMode === TriggerMode.TriggerModeManualWithAutoInit

  // clickable (i.e. trigger button will appear) if it doesn't already have some kind of pending / active build
  let clickable =
    !props.isQueued && // already queued for manual run
    !props.isBuilding && // currently building
    !(isAutoInit && !props.hasBuilt) // waiting to perform its initial build

  let isEmphasized = false
  if (clickable) {
    if (props.hasPendingChanges && isManual) {
      isEmphasized = true
    } else if (!props.hasBuilt && !isAutoInit) {
      isEmphasized = true
    }
  }

  let onClick = useCallback(
    (e: any) => {
      // SidebarTriggerButton is nested in a link,
      // and preventDefault is the standard way to cancel the navigation.
      e.preventDefault()

      // stopPropagation prevents the overview card from opening.
      e.stopPropagation()

      props.onTrigger()
    },
    [props.onTrigger]
  )

  // Add padding to center the icon better.
  let padding = isEmphasized ? "0" : "0 0 0 2px"
  let classes = []
  if (props.isSelected) {
    classes.push("is-selected")
  }
  if (clickable) {
    classes.push("is-clickable")
  }
  if (props.isQueued) {
    classes.push("is-queued")
  }
  if (isManual) {
    classes.push("is-manual")
  }
  return (
    <SidebarTriggerButtonRoot
      onClick={onClick}
      className={classes.join(" ")}
      disabled={!clickable}
      title={triggerTooltip(clickable, isEmphasized, props.isQueued)}
      style={{ padding }}
      analyticsName={"ui.web.triggerResource"}
      analyticsTags={props.analyticsTags}
    >
      {isEmphasized ? <TriggerButtonManualSvg /> : <TriggerButtonSvg />}
    </SidebarTriggerButtonRoot>
  )
}

export default React.memo(SidebarTriggerButton)
