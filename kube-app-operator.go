/*
Copyright 2014 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"

	"github.com/derekparker/delve/pkg/config"
	"github.com/enablecloud/kube-app-operator/operator"

	"github.com/golang/example/stringutil"
)

func main() {
	conf := &config.Config{}
	var eventHandler enablecloud.Handler
	eventHandler = new(enablecloud.Default)
	//switch {
	//case len(conf.Handler.Slack.Channel) > 0 || len(conf.Handler.Slack.Token) > 0:
	//	eventHandler = new(slack.Slack)
	//default:
	//	eventHandler = new(handlers.Default)
	//}

	enablecloud.Start(conf, eventHandler)
	fmt.Println(stringutil.Reverse("!selpmaxe oG ,olleH"))

}
