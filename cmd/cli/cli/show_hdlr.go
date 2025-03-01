// Package cli provides easy-to-use commands to manage, monitor, and utilize AIS clusters.
// This file contains implementation of the top-level `show` command.
/*
 * Copyright (c) 2018-2023, NVIDIA CORPORATION. All rights reserved.
 */
package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/NVIDIA/aistore/api"
	"github.com/NVIDIA/aistore/api/apc"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cluster/meta"
	"github.com/NVIDIA/aistore/cmd/cli/teb"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/cmn/debug"
	"github.com/NVIDIA/aistore/ext/dsort"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/xact"
	"github.com/urfave/cli"
)

type (
	daemonTemplateXactSnaps struct {
		DaemonID  string
		XactSnaps []*cluster.Snap
	}

	targetMpath struct {
		DaemonID  string
		Mpl       *apc.MountpathList
		TargetCDF fs.TargetCDF
	}
)

var (
	showCmdsFlags = map[string][]cli.Flag{
		commandJob: append(
			longRunFlags,
			jsonFlag,
			allJobsFlag,
			regexJobsFlag,
			noHeaderFlag,
			verboseFlag,
			unitsFlag,
			// download and dsort only
			progressFlag,
			dsortLogFlag,
		),
		cmdObject: {
			objPropsFlag, // --props [list]
			allPropsFlag,
			objNotCachedPropsFlag,
			noHeaderFlag,
			jsonFlag,
		},
		cmdCluster: append(
			longRunFlags,
			jsonFlag,
			noHeaderFlag,
		),
		cmdSmap: append(
			longRunFlags,
			jsonFlag,
		),
		cmdBMD: {
			jsonFlag,
		},
		cmdBucket: {
			jsonFlag,
			compactPropFlag,
		},
		cmdConfig: {
			jsonFlag,
		},
		cmdShowRemoteAIS: {
			noHeaderFlag,
			verboseFlag,
			jsonFlag,
		},
		cmdLog: append(
			longRunFlags,
			logSevFlag,
			logFlushFlag,
		),
	}

	showCmd = cli.Command{
		Name:  commandShow,
		Usage: "show configuration, buckets, jobs, etc. - all managed entities in the cluster, and the cluster itself",
		Subcommands: []cli.Command{
			makeAlias(authCmdShow, "", true, commandAuth), // alias for `ais auth show`
			showCmdObject,
			showCmdBucket,
			showCmdCluster,
			showCmdPeformance,
			showCmdStorage,
			showCmdRebalance,
			showCmdConfig,
			showCmdRemoteAIS,
			showCmdJob,
			showCmdLog,
		},
	}

	showCmdStorage = cli.Command{
		Name:      commandStorage,
		Usage:     "show storage usage and utilization, disks and mountpaths",
		ArgsUsage: optionalTargetIDArgument,
		Flags:     storageFlags[commandStorage],
		Action:    showStorageHandler,
		Subcommands: []cli.Command{
			showCmdDisk,
			showCmdMpath,
			showCmdMpathCapacity,
			showCmdStgSummary,
		},
	}
	showCmdObject = cli.Command{
		Name:         cmdObject,
		Usage:        "show object details",
		ArgsUsage:    objectArgument,
		Flags:        showCmdsFlags[cmdObject],
		Action:       showObjectHandler,
		BashComplete: bucketCompletions(bcmplop{separator: true}),
	}
	showCmdCluster = cli.Command{
		Name:         cmdCluster,
		Usage:        "show cluster nodes and utilization",
		ArgsUsage:    showClusterArgument,
		Flags:        showCmdsFlags[cmdCluster],
		Action:       showClusterHandler,
		BashComplete: showClusterCompletions, // NOTE: level 0 hardcoded
		Subcommands: []cli.Command{
			{
				Name:         cmdSmap,
				Usage:        "show Smap (cluster map)",
				ArgsUsage:    optionalNodeIDArgument,
				Flags:        showCmdsFlags[cmdSmap],
				Action:       showSmapHandler,
				BashComplete: suggestAllNodes,
			},
			{
				Name:         cmdBMD,
				Usage:        "show BMD (bucket metadata)",
				ArgsUsage:    optionalNodeIDArgument,
				Flags:        showCmdsFlags[cmdBMD],
				Action:       showBMDHandler,
				BashComplete: suggestAllNodes,
			},
			{
				Name:      cmdConfig,
				Usage:     "show cluster and node configuration",
				ArgsUsage: showClusterConfigArgument,
				Flags:     showCmdsFlags[cmdConfig],
				Action:    showClusterConfigHandler,
			},
			makeAlias(showCmdPeformance, cliName+" "+commandShow+" "+commandPerf, false /*silent*/, cmdShowStats),
		},
	}
	showCmdBucket = cli.Command{
		Name:         cmdBucket,
		Usage:        "show bucket properties",
		ArgsUsage:    bucketAndPropsArgument,
		Flags:        showCmdsFlags[cmdBucket],
		Action:       showBckPropsHandler,
		BashComplete: bucketAndPropsCompletions, // bucketCompletions(),
	}
	showCmdConfig = cli.Command{
		Name:         cmdConfig,
		Usage:        "show CLI, cluster, or node configurations (nodes inherit cluster and have local)",
		ArgsUsage:    showConfigArgument,
		Flags:        showCmdsFlags[cmdConfig],
		Action:       showConfigHandler,
		BashComplete: showConfigCompletions,
	}
	showCmdRemoteAIS = cli.Command{
		Name:      cmdShowRemoteAIS,
		Usage:     "show attached AIS clusters",
		ArgsUsage: "",
		Flags:     showCmdsFlags[cmdShowRemoteAIS],
		Action:    showRemoteAISHandler,
	}

	showCmdLog = cli.Command{
		Name:         cmdLog,
		Usage:        "show log",
		ArgsUsage:    nodeIDArgument,
		Flags:        showCmdsFlags[cmdLog],
		Action:       showNodeLogHandler,
		BashComplete: suggestAllNodes,
	}

	showCmdJob = cli.Command{
		Name:         commandJob,
		Usage:        "show running and finished jobs ('--all' for all, or " + tabHelpOpt + ")",
		ArgsUsage:    jobShowStopWaitArgument,
		Flags:        showCmdsFlags[commandJob],
		Action:       showJobsHandler,
		BashComplete: runningJobCompletions,
	}
)

// args [NAME] [JOB_ID] [NODE_ID] [BUCKET] may:
// - be omitted, in part or in total, and may
// - come in arbitrary order
func showJobsHandler(c *cli.Context) error {
	name, xid, daemonID, bck, err := jobArgs(c, 0, false /*ignore daemonID*/)
	if err != nil {
		return err
	}

	if name == cmdRebalance {
		return showRebalanceHandler(c)
	}

	setLongRunParams(c, 72)

	var l int
	l, err = showJobsDo(c, name, xid, daemonID, bck)
	if err == nil && l == 0 && !flagIsSet(c, allJobsFlag) {
		n, h := qflprn(allJobsFlag), qflprn(cli.HelpFlag)
		fmt.Fprintf(c.App.Writer, "No running jobs. "+
			"Use %s to show all, %s <TAB-TAB> to select, %s for details.\n", n, n, h)
	}
	return err
}

func showJobsDo(c *cli.Context, name, xid, daemonID string, bck cmn.Bck) (int, error) {
	if name == "" && xid != "" {
		name, _ = xid2Name(xid)
	}
	if name != "" || xid != "" {
		return _showJobs(c, name, xid, daemonID, bck, xid == "" /*caption*/)
	}

	// special (best-effort)
	if xid != "" {
		switch name {
		case cmdDownload:
			return _showJobs(c, cmdDownload, xid, daemonID, bck, false)
		case cmdDsort:
			return _showJobs(c, cmdDsort, xid, daemonID, bck, false)
		case commandETL:
			_, otherID := xid2Name(xid)
			return _showJobs(c, commandETL, otherID /*etl name*/, daemonID, bck, false)
		}
	}

	var (
		ll  int
		err error
	)
	names := xact.ListDisplayNames(false /*only-startable*/)
	names = append(names, dsort.DSortName) // NOTE: dsort isn't an x (the only exception)
	sort.Strings(names)
	for _, name = range names {
		l, errV := _showJobs(c, name, "" /*xid*/, daemonID, bck, true)
		if errV != nil {
			actionWarn(c, errV.Error())
			err = errV
		}
		ll += l
	}
	return ll, err
}

func jobCptn(c *cli.Context, name string, onlyActive bool, xid string, byTarget bool) {
	if xid != "" {
		actionCptn(c, jobName(name, xid), "")
		return
	}

	var s string
	if byTarget {
		s = " by target"
	}
	if onlyActive {
		actionCptn(c, name, " jobs"+s+":")
	} else {
		actionCptn(c, name, " jobs"+s+" (including finished):")
	}
}

func _showJobs(c *cli.Context, name, xid, daemonID string, bck cmn.Bck, caption bool) (int, error) {
	switch name {
	case cmdDownload:
		return showDownloads(c, xid, caption)
	case cmdDsort:
		return showDsorts(c, xid, caption)
	case commandETL:
		return showETLs(c, xid, caption)
	default:
		var (
			// finished or not, always try to show when xid provided
			all         = flagIsSet(c, allJobsFlag) || xact.IsValidUUID(xid)
			onlyActive  = !all
			xactKind, _ = xact.GetKindName(name)
			regexStr    = parseStrFlag(c, regexJobsFlag)
			xargs       = xact.ArgsMsg{
				ID:          xid,
				Kind:        xactKind,
				DaemonID:    daemonID,
				Bck:         bck,
				OnlyRunning: onlyActive,
			}
		)
		if regexStr != "" {
			regex, err := regexp.Compile(regexStr)
			if err != nil {
				actionWarn(c, err.Error())
				regex = nil
			}
			if regex != nil && !regex.MatchString(name) && !regex.MatchString(xactKind) {
				return 0, nil
			}
		}
		return xactList(c, xargs, caption)
	}
}

func showDownloads(c *cli.Context, id string, caption bool) (int, error) {
	if id == "" { // list all download jobs
		return downloadJobsList(c, parseStrFlag(c, regexJobsFlag), caption)
	}
	// display status of a download job identified by its JOB_ID
	return 1, downloadJobStatus(c, id)
}

func showDsorts(c *cli.Context, id string, caption bool) (int, error) {
	var (
		usejs      = flagIsSet(c, jsonFlag)
		onlyActive = !flagIsSet(c, allJobsFlag)
	)
	if id == "" {
		list, err := api.ListDSort(apiBP, parseStrFlag(c, regexJobsFlag), onlyActive)
		l := len(list)
		if err != nil || l == 0 {
			return l, V(err)
		}
		if caption {
			jobCptn(c, cmdDsort, onlyActive, id, false)
		}
		return l, dsortJobsList(c, list, usejs)
	}

	// ID-ed dsort
	return 1, dsortJobStatus(c, id)
}

func showClusterHandler(c *cli.Context) error {
	var (
		what, sid string
		daeType   string
	)
	if c.NArg() > 0 {
		what = c.Args().Get(0)
		if node, _, errV := getNode(c, what); errV == nil {
			sid, what = node.ID(), ""
			daeType = node.Type()
		}
	}
	if c.NArg() > 1 {
		arg := c.Args().Get(1)
		if sid != "" { // not "what"
			return incorrectUsageMsg(c, "", arg)
		}
		node, _, err := getNode(c, arg)
		if err != nil {
			return err
		}
		sid, daeType = node.ID(), node.Type()
	}

	setLongRunParams(c)

	smap, tstatusMap, pstatusMap, err := fillNodeStatusMap(c, daeType)
	if err != nil {
		return err
	}
	cluConfig, err := api.GetClusterConfig(apiBP)
	if err != nil {
		return V(err)
	}
	if sid != "" {
		return cluDaeStatus(c, smap, tstatusMap, pstatusMap, cluConfig, sid)
	}
	return cluDaeStatus(c, smap, tstatusMap, pstatusMap, cluConfig, what)
}

func xactList(c *cli.Context, xargs xact.ArgsMsg, caption bool) (int, error) {
	// override the caller's choice if explicitly identified
	if xargs.ID != "" {
		debug.Assert(xact.IsValidUUID(xargs.ID), xargs.ID)
		xargs.OnlyRunning = false
	}

	xs, err := queryXactions(xargs)
	if err != nil {
		return 0, err
	}
	var numSnaps int
	for _, snaps := range xs {
		numSnaps += len(snaps)
	}
	if numSnaps == 0 {
		return 0, nil
	}

	var (
		ll           int
		allXactKinds = extractXactKinds(xs)
	)
	for _, xactKind := range allXactKinds {
		xactIDs := extractXactIDsForKind(xs, xactKind)
		for _, xid := range xactIDs {
			xargs.Kind, xargs.ID = xactKind, xid
			l, err := xlistByKindID(c, xargs, caption, xs)
			if err != nil {
				actionWarn(c, err.Error())
			}
			ll += l
		}
	}
	return ll, nil
}

func xlistByKindID(c *cli.Context, xargs xact.ArgsMsg, caption bool, xs xact.MultiSnap) (int, error) {
	// first, extract snaps for: xargs.ID, Kind
	filteredXs := make(xact.MultiSnap, 8)
	for tid, snaps := range xs {
		for _, snap := range snaps {
			if snap.ID != xargs.ID {
				continue
			}
			debug.Assert(snap.Kind == xargs.Kind)
			if _, ok := filteredXs[tid]; !ok {
				filteredXs[tid] = make([]*cluster.Snap, 0, 8)
			}
			filteredXs[tid] = append(filteredXs[tid], snap)
		}
	}

	// second, filteredXs => dts templates
	var (
		fromToBck, haveBck bool
		dts                = make([]daemonTemplateXactSnaps, 0, len(filteredXs))
	)
	for tid, snaps := range filteredXs {
		if len(snaps) == 0 {
			continue
		}
		if xargs.DaemonID != "" && xargs.DaemonID != tid {
			continue
		}
		if !snaps[0].SrcBck.IsEmpty() {
			debug.Assert(!snaps[0].DstBck.IsEmpty())
			fromToBck = true
		} else if !snaps[0].Bck.IsEmpty() {
			haveBck = true
		}
		dts = append(dts, daemonTemplateXactSnaps{DaemonID: tid, XactSnaps: snaps})
	}
	sort.Slice(dts, func(i, j int) bool {
		return dts[i].DaemonID < dts[j].DaemonID // ascending by node id/name
	})

	_, xname := xact.GetKindName(xargs.Kind)
	if caption {
		jobCptn(c, xname, xargs.OnlyRunning, xargs.ID, xargs.DaemonID != "")
	}

	l := len(dts)
	var (
		err error

		usejs       = flagIsSet(c, jsonFlag)
		hideHeader  = flagIsSet(c, noHeaderFlag)
		units, errU = parseUnitsFlag(c, unitsFlag)
	)
	if errU != nil {
		actionWarn(c, errU.Error())
		units = ""
	}
	opts := teb.Opts{AltMap: teb.FuncMapUnits(units), UseJSON: usejs}
	switch xargs.Kind {
	case apc.ActECGet:
		if hideHeader {
			return l, teb.Print(dts, teb.XactECGetNoHdrTmpl, opts)
		}
		return l, teb.Print(dts, teb.XactECGetTmpl, opts)
	case apc.ActECPut:
		if hideHeader {
			return l, teb.Print(dts, teb.XactECPutNoHdrTmpl, opts)
		}
		return l, teb.Print(dts, teb.XactECPutTmpl, opts)
	default:
		switch {
		case fromToBck && hideHeader:
			err = teb.Print(dts, teb.XactNoHdrFromToTmpl, opts)
		case fromToBck:
			err = teb.Print(dts, teb.XactFromToTmpl, opts)
		case haveBck && hideHeader:
			err = teb.Print(dts, teb.XactNoHdrBucketTmpl, opts)
		case haveBck:
			err = teb.Print(dts, teb.XactBucketTmpl, opts)
		default:
			if hideHeader {
				err = teb.Print(dts, teb.XactNoHdrNoBucketTmpl, opts)
			} else {
				err = teb.Print(dts, teb.XactNoBucketTmpl, opts)
			}
		}
	}
	if err != nil || !flagIsSet(c, verboseFlag) {
		return l, err
	}

	// show in/out stats when verbose
	for _, di := range dts {
		if len(dts[0].XactSnaps) == 0 {
			continue
		}
		props := flattenXactStats(di.XactSnaps[0], units)
		_, name := xact.GetKindName(di.XactSnaps[0].Kind)
		debug.Assert(name != "", di.XactSnaps[0].Kind)
		actionCptn(c, meta.Tname(di.DaemonID)+": ", fmt.Sprintf("%s[%s] stats", name, di.XactSnaps[0].ID))
		if err := teb.Print(props, teb.PropsSimpleTmpl, teb.Jopts(usejs)); err != nil {
			return l, err
		}
	}
	return l, nil
}

func showObjectHandler(c *cli.Context) (err error) {
	if c.NArg() < 1 {
		return missingArgumentsError(c, "object name in the form "+objectArgument)
	}
	fullObjName := c.Args().Get(0)
	bck, object, err := parseBckObjURI(c, fullObjName, false)
	if err != nil {
		return err
	}
	if _, err := headBucket(bck, true /* don't add */); err != nil {
		return err
	}
	return showObjProps(c, bck, object)
}

func showBckPropsHandler(c *cli.Context) (err error) {
	return showBucketProps(c)
}

func showSmapHandler(c *cli.Context) error {
	var (
		sid              string
		node, sname, err = arg0Node(c)
	)
	if err != nil {
		return err
	}

	setLongRunParams(c)

	if node != nil {
		sid = node.ID()
		actionCptn(c, "Cluster map from: ", sname)
	}
	smap, err := getClusterMap(c)
	if err != nil {
		return err // cannot happen
	}
	return smapFromNode(smap, sid, flagIsSet(c, jsonFlag))
}

func showBMDHandler(c *cli.Context) (err error) {
	return getBMD(c)
}

func showClusterConfigHandler(c *cli.Context) (err error) {
	return showClusterConfig(c, c.Args().Get(0))
}

func showConfigHandler(c *cli.Context) (err error) {
	if c.NArg() == 0 {
		return incorrectUsageMsg(c, "missing arguments (hint: "+tabtab+")")
	}
	if c.Args().Get(0) == cmdCLI {
		return showCfgCLI(c)
	}
	if c.Args().Get(0) == cmdCluster {
		return showClusterConfig(c, c.Args().Get(1))
	}
	return showNodeConfig(c)
}

func showClusterConfig(c *cli.Context, section string) error {
	var (
		usejs          = flagIsSet(c, jsonFlag)
		cluConfig, err = api.GetClusterConfig(apiBP)
	)
	if err != nil {
		return err
	}

	if usejs && section != "" {
		if printSectionJSON(c, cluConfig, section) {
			return nil
		}
		usejs = false
	}

	if usejs {
		return teb.Print(cluConfig, "", teb.Jopts(usejs))
	}
	flat := flattenConfig(cluConfig, section)
	err = teb.Print(flat, teb.ConfigTmpl)
	if err == nil && section == "" {
		msg := fmt.Sprintf("(Hint: use '[SECTION] %s' to show config section(s), see %s for details)",
			flprn(jsonFlag), qflprn(cli.HelpFlag))
		actionDone(c, msg)
	}
	return err
}

func showNodeConfig(c *cli.Context) error {
	var (
		section string
		scope   string
		usejs   = flagIsSet(c, jsonFlag)
	)
	if c.NArg() == 0 {
		return missingArgumentsError(c, c.Command.ArgsUsage)
	}
	node, sname, err := getNode(c, c.Args().Get(0))
	if err != nil {
		return err
	}
	config, err := api.GetDaemonConfig(apiBP, node)
	if err != nil {
		return V(err)
	}

	data := struct {
		ClusterConfigDiff []propDiff
		LocalConfigPairs  nvpairList
	}{}
	for _, a := range c.Args().Tail() {
		if a == scopeAll || a == cfgScopeInherited || a == cfgScopeLocal {
			if scope != "" {
				return incorrectUsageMsg(c, "... %s %s ...", scope, a)
			}
			scope = a
		} else {
			if scope == "" {
				return incorrectUsageMsg(c, "... %v ...", c.Args().Tail())
			}
			if section != "" {
				return incorrectUsageMsg(c, "... %s %s ...", section, a)
			}
			section = a
			if i := strings.IndexByte(section, '='); i > 0 {
				section = section[:i] // when called to show set-config result (same as above)
			}
		}
	}

	if usejs {
		opts := teb.Jopts(true)
		warn := "option " + qflprn(jsonFlag) + " won't show node <=> cluster configuration differences, if any."
		switch scope {
		case cfgScopeLocal:
			if section == "" {
				return teb.Print(&config.LocalConfig, "", opts)
			}
			if !printSectionJSON(c, &config.LocalConfig, section) {
				fmt.Fprintln(c.App.Writer)
			}
			return nil
		case cfgScopeInherited:
			actionWarn(c, warn)
			if section == "" {
				return teb.Print(&config.ClusterConfig, "", opts)
			}
			if !printSectionJSON(c, &config.ClusterConfig, section) {
				fmt.Fprintln(c.App.Writer)
			}
			return nil
		default: // cfgScopeAll
			if section == "" {
				actionCptn(c, sname, " local config:")
				if err := teb.Print(&config.LocalConfig, "", opts); err != nil {
					return err
				}
				fmt.Fprintln(c.App.Writer)
				actionCptn(c, sname, " inherited config:")
				actionWarn(c, warn)
				return teb.Print(&config.ClusterConfig, "", opts)
			}
			// fall through on purpose
		}
	}

	usejs = false

	// fill-in `data`
	switch scope {
	case cfgScopeLocal:
		data.LocalConfigPairs = flattenConfig(config.LocalConfig, section)
	default: // cfgScopeInherited | cfgScopeAll
		cluConf, err := api.GetClusterConfig(apiBP)
		if err != nil {
			return V(err)
		}
		// diff cluster <=> this node
		flatNode := flattenConfig(config.ClusterConfig, section)
		flatCluster := flattenConfig(cluConf, section)
		data.ClusterConfigDiff = diffConfigs(flatNode, flatCluster)
		if scope == cfgScopeAll {
			data.LocalConfigPairs = flattenConfig(config.LocalConfig, section)
		}
	}
	// show "flat" diff-s
	if len(data.LocalConfigPairs) == 0 && len(data.ClusterConfigDiff) == 0 {
		fmt.Fprintf(c.App.Writer, "PROPERTY\t VALUE\n\n")
		return nil
	}
	err = teb.Print(data, teb.DaemonConfigTmpl, teb.Jopts(usejs))

	if err == nil && section == "" {
		msg := fmt.Sprintf("(Hint: to show specific section(s), use 'inherited [SECTION]' or 'all [SECTION]' with or without %s)",
			flprn(jsonFlag))
		actionDone(c, msg)
	}
	return err
}

func showNodeLogHandler(c *cli.Context) error {
	if c.NArg() < 1 {
		return missingArgumentsError(c, c.Command.ArgsUsage)
	}
	node, sname, err := getNode(c, c.Args().Get(0))
	if err != nil {
		return err
	}

	firstIteration := setLongRunParams(c, 0)

	sev := strings.ToLower(parseStrFlag(c, logSevFlag))
	if sev != "" {
		switch sev[0] {
		case apc.LogInfo[0], apc.LogWarn[0], apc.LogErr[0]:
		default:
			return fmt.Errorf("invalid log severity, expecting empty string or one of: %s, %s, %s",
				apc.LogInfo, apc.LogWarn, apc.LogErr)
		}
	}
	if firstIteration && flagIsSet(c, logFlushFlag) {
		var (
			flushRate = parseDurationFlag(c, logFlushFlag)
			nvs       = make(cos.StrKVs)
		)
		config, err := api.GetDaemonConfig(apiBP, node)
		if err != nil {
			return V(err)
		}
		if config.Log.FlushTime.D() != flushRate {
			nvs[nodeLogFlushName] = flushRate.String()
			if err := api.SetDaemonConfig(apiBP, node.ID(), nvs, true /*transient*/); err != nil {
				return V(err)
			}
			warn := fmt.Sprintf("run 'ais config node %s inherited %s %s' to change it back",
				sname, nodeLogFlushName, config.Log.FlushTime)
			actionWarn(c, warn)
			time.Sleep(2 * time.Second)
			fmt.Fprintln(c.App.Writer)
		}
	}

	args := api.GetLogInput{Writer: os.Stdout, Severity: sev, Offset: getLongRunOffset(c)}
	readsize, err := api.GetDaemonLog(apiBP, node, args)
	if err == nil {
		addLongRunOffset(c, readsize)
	}
	return V(err)
}

func showRemoteAISHandler(c *cli.Context) error {
	all, err := api.GetRemoteAIS(apiBP)
	if err != nil {
		return V(err)
	}
	tw := &tabwriter.Writer{}
	tw.Init(c.App.Writer, 0, 8, 2, ' ', 0)
	if !flagIsSet(c, noHeaderFlag) {
		fmt.Fprintln(tw, "UUID\tURL\tAlias\tPrimary\tSmap\tTargets\tUptime")
	}
	for _, ra := range all.A {
		uptime := teb.UnknownStatusVal
		bp := api.BaseParams{
			Client: defaultHTTPClient,
			URL:    ra.URL,
			Token:  loggedUserToken,
			UA:     ua,
		}
		if clutime, _, err := api.HealthUptime(bp); err == nil {
			ns, _ := strconv.ParseInt(clutime, 10, 64)
			uptime = time.Duration(ns).String()
		}
		if ra.Smap != nil {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\tv%d\t%d\t%s\n",
				ra.UUID, ra.URL, ra.Alias, ra.Smap.Primary, ra.Smap.Version, ra.Smap.CountTargets(), uptime)
		} else {
			url := ra.URL
			if len(url) > 0 && url[0] == '[' && !strings.Contains(url, " ") {
				url = strings.Replace(url, "[", "", 1)
				url = strings.Replace(url, "]", "", 1)
			}
			fmt.Fprintf(tw, "<%s>\t%s\t%s\t%s\t%s\t%s\t%s\n", ra.UUID, url, ra.Alias,
				teb.UnknownStatusVal, teb.UnknownStatusVal, teb.UnknownStatusVal, uptime)

			warn := fmt.Sprintf(warnRemAisOffline, url)
			actionWarn(c, warn+"\n")
		}
	}
	tw.Flush()

	if flagIsSet(c, verboseFlag) {
		for _, ra := range all.A {
			if ra.Smap == nil {
				// if ra.UUID == apc.RemAisDefunct { // DEBUG
				// }
				continue
			}
			fmt.Fprintln(c.App.Writer)
			actionCptn(c, ra.Alias+"["+ra.UUID+"]", " cluster map:")
			err := smapFromNode(ra.Smap, "" /*daemonID*/, flagIsSet(c, jsonFlag))
			if err != nil {
				actionWarn(c, err.Error())
			}
		}
	}
	return nil
}
