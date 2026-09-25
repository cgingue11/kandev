"use client";

import * as React from "react";
import type { PluginActionGroupProps, PluginActionProps } from "@kandev/plugin-sdk";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kandev/ui/tooltip";
import { SurfaceAction } from "@/components/actions/surface-action";
import { surfaceActionGroupClassName } from "@/components/actions/surface-action-styles";
import { useResponsiveBreakpoint } from "@/hooks/use-responsive-breakpoint";
import { usePluginActionSurface } from "./plugin-action-surface";

export function PluginAction(props: PluginActionProps) {
  const surface = usePluginActionSurface();
  const { isFinePointer } = useResponsiveBreakpoint();
  if (!surface) {
    // i18n-exempt: developer misuse error, not user-facing copy.
    throw new Error("host.ui.Action must be rendered inside a supported plugin slot.");
  }

  const label = typeof props.label === "string" ? props.label : "";
  const text = typeof props.text === "string" ? props.text : undefined;
  const badge = typeof props.badge === "string" ? props.badge : undefined;
  const iconOnly = !text;
  let tooltip = props.tooltip || undefined;
  if (props.tooltip === undefined && iconOnly) tooltip = label;
  const showTooltip = isFinePointer && surface.presentation !== "mobile";

  const action = (
    <SurfaceAction
      surface={surface.surface}
      presentation={surface.presentation}
      label={label}
      icon={props.icon as React.ReactNode}
      text={text}
      badge={badge}
      tone={props.tone}
      pressed={props.pressed}
      disabled={props.disabled}
      busy={props.busy}
      ref={props.ref as React.Ref<HTMLButtonElement>}
      id={props.id}
      aria-expanded={props["aria-expanded"]}
      aria-controls={props["aria-controls"]}
      aria-haspopup={props["aria-haspopup"]}
      aria-describedby={props["aria-describedby"]}
      data-testid={props["data-testid"]}
      data-state={props["data-state"]}
      data-side={props["data-side"]}
      data-align={props["data-align"]}
      data-disabled={props["data-disabled"]}
      onClick={props.onClick as React.MouseEventHandler<HTMLButtonElement> | undefined}
      onFocus={props.onFocus as React.FocusEventHandler<HTMLButtonElement> | undefined}
      onBlur={props.onBlur as React.FocusEventHandler<HTMLButtonElement> | undefined}
      onKeyDown={props.onKeyDown as React.KeyboardEventHandler<HTMLButtonElement> | undefined}
      onPointerDown={
        props.onPointerDown as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onPointerUp={props.onPointerUp as React.PointerEventHandler<HTMLButtonElement> | undefined}
      onPointerCancel={
        props.onPointerCancel as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onPointerEnter={
        props.onPointerEnter as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onPointerMove={
        props.onPointerMove as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onPointerLeave={
        props.onPointerLeave as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onLostPointerCapture={
        props.onLostPointerCapture as React.PointerEventHandler<HTMLButtonElement> | undefined
      }
      onMouseEnter={props.onMouseEnter as React.MouseEventHandler<HTMLButtonElement> | undefined}
      onMouseLeave={props.onMouseLeave as React.MouseEventHandler<HTMLButtonElement> | undefined}
    />
  );

  return tooltip && showTooltip ? (
    <Tooltip>
      <TooltipTrigger asChild>{action}</TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  ) : (
    action
  );
}

export function PluginActionGroup(props: PluginActionGroupProps) {
  const children = React.Children.toArray(props.children as React.ReactNode);
  if (children.length === 0) return null;

  const surface = usePluginActionSurface();
  if (!surface) {
    // i18n-exempt: developer misuse error, not user-facing copy.
    throw new Error("host.ui.ActionGroup must be rendered inside a supported plugin slot.");
  }

  return (
    <div
      className={surfaceActionGroupClassName(surface.surface, surface.presentation)}
      role={props.label ? "group" : undefined}
      aria-label={props.label}
      data-slot="surface-action-group"
      data-surface={surface.surface}
    >
      {children}
    </div>
  );
}
